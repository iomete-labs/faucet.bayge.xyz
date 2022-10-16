package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"

	_ "github.com/mattn/go-sqlite3"

	ethCommon "github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	ethCrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// EthereumDecimals to divide everything by before scanning how much to
// send in the database
const EthereumDecimals = 1e18

const (
	// EnvPrivateKey to use when sending amounts to users
	EnvPrivateKey = "BAY_ETH_PRIVATE_KEY"

	// EnvRpcUrl to connect to to send amounts with with Ethereum RPC
	EnvRpcUrl = "BAY_ETH_RPC_URL"

	// EnvDatabasePath to load the sqlite database from
	EnvDatabasePath = "BAY_DATABASE_PATH"

	// EnvListenAddress to host the webserver on
	EnvListenAddress = "BAY_HTTP_LISTEN_ADDR"
)

const nullAddress = "0x0000000000000000000000000000000000000000"

type outgoingTransaction struct {
	reply   chan string
	address ethCommon.Address
	amount  *big.Int
}

func writeBadRequest(w http.ResponseWriter, reason string) {
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, "bad request: %v", reason)
}

func main() {
	var (
		privateKey_   = os.Getenv(EnvPrivateKey)
		rpcUrl        = os.Getenv(EnvRpcUrl)
		databasePath  = os.Getenv(EnvDatabasePath)
		listenAddress = os.Getenv(EnvListenAddress)
	)

	ethereumDecimals := new(big.Int).SetInt64(EthereumDecimals)

	privateKey, err := ethCrypto.HexToECDSA(privateKey_)

	if err != nil {
		log.Fatalf(
			"Failed to convert the passed private key to an internal representation! %v",
			err,
		)
	}

	db, err := sql.Open("sqlite3", databasePath)

	if err != nil {
		log.Fatalf(
			"Failed to open the database given! %v",
			err,
		)
	}

	defer db.Close()

	client, err := ethclient.Dial(rpcUrl)

	if err != nil {
		log.Fatalf(
			"Failed to connect to the RPC endpoint! %v",
			err,
		)
	}

	defer client.Close()

	chainId, err := client.ChainID(context.Background())

	if err != nil {
		log.Fatalf(
			"Failed to get the chain id using the RPC provider! %v",
			err,
		)
	}

	publicKey := privateKey.PublicKey

	senderAddress := ethCrypto.PubkeyToAddress(publicKey)

	outgoingTransactions := make(chan outgoingTransaction, 0)

	go func() {
		for transaction := range outgoingTransactions {
			var (
				reply   = transaction.reply
				address = transaction.address
				amount  = transaction.amount
			)

			nonce, err := client.NonceAt(context.Background(), senderAddress, nil)

			if err != nil {
				log.Fatalf(
					"Failed to get the nonce! %v",
					err,
				)
			}

			// hardcoding since suggesting it isn't working reliably

			txData := &ethTypes.DynamicFeeTx{
				To:        &address,
				Value:     amount,
				Nonce:     nonce + 1,
				Gas:       21000,
				GasTipCap: new(big.Int).SetInt64(1000000000),  // maxPriorityFeePerGas
				GasFeeCap: new(big.Int).SetInt64(14300000000), // maxFeePerGas
			}

			transaction := ethTypes.NewTx(txData)

			signer := ethTypes.NewLondonSigner(chainId)

			signed, err := ethTypes.SignTx(transaction, signer, privateKey)

			if err != nil {
				log.Fatalf(
					"Failed to sign the faucet transaction! %v",
					err,
				)
			}

			err = client.SendTransaction(context.Background(), signed)

			if err != nil {
				log.Fatalf(
					"Failed to send a transaction! %v",
					err,
				)
			}

			reply <- signed.Hash().Hex()
		}
	}()

	http.HandleFunc("/request", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			writeBadRequest(w, "not post")
			return
		}

		if err := r.ParseForm(); err != nil {
			writeBadRequest(w, "couldnt parse")
			return
		}

		var (
			secret     = r.PostForm.Get("secret")
			recipient_ = r.PostForm.Get("recipient")
		)

		if secret == "" || recipient_ == "" {
			writeBadRequest(w, "secret or recipient empty")
			return
		}

		amount_, beenUsed, err := getSecretInfoAndInvalidate(db, secret)

		if err != nil {
			log.Printf(
				"Failed to get a user's secret info! %v",
				err,
			)

			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		if beenUsed {
			fmt.Fprintf(w, "already used")
			return
		}

		amount := new(big.Int).SetInt64(int64(amount_))

		amount.Mul(amount, ethereumDecimals)

		recipient := ethCommon.HexToAddress(recipient_)

		if recipient == ethCommon.HexToAddress(nullAddress) {
			fmt.Fprintf(w, "null address")
			return
		}

		reply := make(chan string)

		outgoingTransactions <- outgoingTransaction{
			reply:   reply,
			address: recipient,
			amount:  amount,
		}

		fmt.Fprint(w, <-reply)
	})

	panic(http.ListenAndServe(listenAddress, nil))
}
