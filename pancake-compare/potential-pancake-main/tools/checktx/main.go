package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"solana-pump-bot/internal/blockchain"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run ./tools/checktx <TX_SIGNATURE>")
		fmt.Println("Example: go run ./tools/checktx 2gHc4gtPHJgVJhccGytQqivvETZoyfiAu12UTE3vN4v6WPz3mGmPGmwxS7NwbXcv28NAQP6Re8rdi2XS9tU6rMRs")
		os.Exit(1)
	}

	txSig := os.Args[1]

	fmt.Println("📊 TX STATUS CHECKER")
	fmt.Println("===================")
	fmt.Printf("TX: %s\n\n", txSig)

	// RPC
	rpc := blockchain.NewRPCClient(
		"https://rpc.shyft.to?api_key=48KZbYxP-9e9SpqR",
		"https://mainnet.helius-rpc.com/?api-key=465a28e0-e3b3-4991-8878-0e7adbb78f81",
		"",
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := rpc.CheckTransaction(ctx, txSig)
	if err != nil {
		fmt.Printf("❌ RPC Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(result.String())
	fmt.Println("")

	if result.Status == "FAILED" {
		fmt.Println("📋 ERROR DETAILS:")
		fmt.Printf("%+v\n", result.ErrorDetails)
	}

	if result.Status == "SUCCESS" {
		fmt.Printf("Slot: %d\n", result.Slot)
		fmt.Printf("Confirmations: %d\n", result.Confirmations)
		fmt.Printf("Status: %s\n", result.ConfirmationStatus)
	}
}
