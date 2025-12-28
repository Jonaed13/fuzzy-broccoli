package analytics

import (
	"encoding/csv"
	"fmt"
	"os"
	"time"

	"solana-pump-bot/internal/storage"
)

// ExportTradesToCSV exports all trades to a CSV file
func ExportTradesToCSV(db *storage.DB, path string) error {
	trades, err := db.GetRecentTrades(10000) // Get all trades
	if err != nil {
		return fmt.Errorf("failed to get trades: %w", err)
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Header
	if err := writer.Write([]string{
		"ID", "Mint", "Token", "Side", "Amount SOL", "Entry%", "Exit%", "PnL%", "Duration(s)", "Entry TX", "Exit TX", "Timestamp",
	}); err != nil {
		return err
	}

	// Rows
	for _, t := range trades {
		row := []string{
			fmt.Sprintf("%d", t.ID),
			t.Mint,
			t.TokenName,
			t.Side,
			fmt.Sprintf("%.4f", t.AmountSol),
			fmt.Sprintf("%.2f", t.EntryValue),
			fmt.Sprintf("%.2f", t.ExitValue),
			fmt.Sprintf("%.2f", t.PnL),
			fmt.Sprintf("%d", t.Duration),
			t.EntryTxSig,
			t.ExitTxSig,
			time.Unix(t.Timestamp, 0).Format(time.RFC3339),
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}

	return nil
}
