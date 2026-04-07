// reset-owner-key deletes the stored owner public key from the SoHoLINK
// database so that the next node startup will generate and print a fresh keypair.
//
// Usage:
//
//	reset-owner-key.exe [db-path]
//
// If db-path is omitted, the default AppData location is used.
package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func main() {
	dbPath := ""
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	} else {
		appData := os.Getenv("LOCALAPPDATA")
		if appData == "" {
			appData = os.Getenv("APPDATA")
		}
		dbPath = filepath.Join(appData, "SoHoLINK", "data", "soholink.db")
	}

	fmt.Printf("Opening database: %s\n", dbPath)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening DB: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Show current owner public key before deletion.
	var pubKey string
	row := db.QueryRow("SELECT value FROM node_info WHERE key = 'owner_public_key'")
	if err := row.Scan(&pubKey); err == nil {
		fmt.Printf("Current owner_public_key: %s\n", pubKey)
	} else {
		fmt.Println("No owner_public_key found in database.")
	}

	// Delete the owner public key — next startup will regenerate and print the new private key.
	res, err := db.Exec("DELETE FROM node_info WHERE key = 'owner_public_key'")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting key: %v\n", err)
		os.Exit(1)
	}
	n, _ := res.RowsAffected()
	fmt.Printf("Deleted %d row(s).\n", n)
	fmt.Println()
	fmt.Println("Done. Restart SoHoLINK — the new private key will be printed to the log.")
	fmt.Println("SAVE IT immediately before it gets rotated.")
}
