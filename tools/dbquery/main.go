package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	dbPath := "data/trades.db"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Printf("Error opening DB: %v\n", err)
		return
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT token_name, value, unit, signal_type, timestamp 
		FROM signals 
		ORDER BY timestamp DESC 
		LIMIT 15
	`)
	if err != nil {
		fmt.Printf("Query error: %v\n", err)
		return
	}
	defer rows.Close()

	fmt.Println("TOKEN\t\tVALUE\tUNIT\tTYPE\t\tTIMESTAMP")
	fmt.Println("─────────────────────────────────────────────────────")
	for rows.Next() {
		var token, unit, sigType string
		var value float64
		var ts int64
		rows.Scan(&token, &value, &unit, &sigType, &ts)
		fmt.Printf("%-12s\t%.1f\t%s\t%-10s\t%d\n", token, value, unit, sigType, ts)
	}
}
