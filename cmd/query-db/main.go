package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("[!] Failed to determine home directory: %v\n", err)
		os.Exit(1)
	}
	dbPath := filepath.Join(homeDir, "AppData", "Local", "SoHoLINK", "data", "soholink.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		fmt.Printf("[!] Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	fmt.Println("[*] Querying node_info table...")
	rows, err := db.Query("SELECT key, value FROM node_info WHERE key LIKE '%owner%' OR key LIKE '%public%' OR key LIKE '%key%'")
	if err != nil {
		fmt.Printf("[!] Query failed: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()

	var key, value string
	for rows.Next() {
		if err := rows.Scan(&key, &value); err != nil {
			fmt.Printf("[!] Scan failed: %v\n", err)
			continue
		}
		fmt.Printf("%s = %s\n", key, truncate(value, 80))
	}
}

func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
