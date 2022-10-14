package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"

	_ "github.com/mattn/go-sqlite3"

	ethAbi "github.com/ethereum/go-ethereum/accounts/abi"
	ethAbiBind "github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethCommon "github.com/ethereum/go-ethereum/common"
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

	// EnvContractAddress to use the transfer function from
	EnvContractAddress = "BAY_ETH_CONTRACT_ADDR"

	// EnvDatabasePath to load the sqlite database from
	EnvDatabasePath = "BAY_DATABASE_PATH"

	// EnvListenAddress to host the webserver on
	EnvListenAddress = "BAY_HTTP_LISTEN_ADDR"
)

const nullAddress = "0x0000000000000000000000000000000000000000"

const transferAbiString = `
[
  {
    "constant": false,
    "name": "transfer",
    "payable": false,
    "stateMutability": "nonpayable",
    "type": "function",
    "inputs": [
      {
        "name": "to",
        "type": "address"
      },
      {
        "name": "value",
        "type": "uint256"
      }
    ]
  }
]
`

func writeBadRequest(w http.ResponseWriter) {
	fmt.Fprintf(w, "bad request")
	w.WriteHeader(http.StatusBadRequest)
}

func main() {
	var (
		privateKey_      = os.Getenv(EnvPrivateKey)
		rpcUrl           = os.Getenv(EnvRpcUrl)
		contractAddress_ = os.Getenv(EnvContractAddress)
		databasePath     = os.Getenv(EnvDatabasePath)
		listenAddress    = os.Getenv(EnvListenAddress)
	)

	transferAbi, err := ethAbi.JSON(strings.NewReader(transferAbiString))

	if err != nil {
		log.Fatalf(
			"Failed to open the JSON reader for the transfer ABI! %v",
			err,
		)
	}

	ethereumDecimals := new(big.Int).SetInt64(EthereumDecimals)

	privateKey, err := ethCrypto.HexToECDSA(privateKey_)

	if err != nil {
		log.Fatalf(
			"Failed to convert the passed private key to an internal representation! %v",
			err,
		)
	}

	contractAddress := ethCommon.HexToAddress(contractAddress_)

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
			"Failed to get the chain ID for the RPC given! %v",
			err,
		)
	}

	transferOpts, err := ethAbiBind.NewKeyedTransactorWithChainID(
		privateKey,
		chainId,
	)

	if err != nil {
		log.Fatalf(
			"Failed to create a new keyed constructor with chain id! %v",
			err,
		)
	}

	boundContract := ethAbiBind.NewBoundContract(
		contractAddress,
		transferAbi,
		client,
		client,
		client,
	)

	http.HandleFunc("/request", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			writeBadRequest(w)
			return
		}

		if err := r.ParseForm(); err != nil {
			writeBadRequest(w)
			return
		}

		var (
			secret     = r.Form.Get("secret")
			recipient_ = r.Form.Get("recipient")
		)

		if secret == "" || recipient_ == "" {
			writeBadRequest(w)
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

		amount.Quo(amount, ethereumDecimals)

		recipient := ethCommon.HexToAddress(recipient_)

		if recipient == ethCommon.HexToAddress(nullAddress) {
			fmt.Fprintf(w, "null address")
			return
		}

		callOpts := ethAbiBind.CallOpts{
			Pending: false,
			Context: context.Background(),
		}

		var results []interface{}

		err = boundContract.Call(
			&callOpts,
			&results,
			"transfer",
			recipient,
			amount,
		)

		if err != nil {
			log.Printf(
				"Failed to simulate the function for testing faucet sending to address %#v! %v",
				recipient_,
				err,
			)

			fmt.Fprintf(w, "%v", err)

			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		transaction, err := boundContract.Transact(
			transferOpts,
			"transfer",
			recipient,
			amount,
		)

		if err != nil {
			log.Fatalf(
				"Failed to transfer an amount! %v",
				err,
			)
		}

		transactionHashHex := transaction.Hash().Hex()

		fmt.Fprintf(w, transactionHashHex)

		log.Printf(
			"Transaction hash for transfer just made is %s!",
			transactionHashHex,
		)
	})

	http.ListenAndServe(listenAddress, nil)
}
