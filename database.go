package main

import (
	"database/sql"
	"fmt"
)

// getSecretInfoAndInvalidate the secret by setting the used field to
// true to prevent abuse
func getSecretInfoAndInvalidate(db *sql.DB, secret string) (amount int, beenUsed bool, err error) {
	query := `
UPDATE keys SET been_used = true
WHERE key_secret = $1
RETURNING amount, been_used`

	row := db.QueryRow(query, secret)

	if err := row.Err(); err != nil {
		return 0, false, fmt.Errorf(
			"failed to query any user secrets: %v",
			err,
		)
	}

	if err := row.Scan(&amount, &beenUsed); err != nil {
		return 0, false, fmt.Errorf(
			"failed to scan the secret row: %v",
			err,
		)
	}

	return
}
