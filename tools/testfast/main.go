package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"solana-pump-bot/internal/blockchain"
	"solana-pump-bot/internal/config"
	"solana-pump-bot/internal/jupiter"
	signalPkg "solana-pump-bot/internal/signal"
	"solana-pump-bot/internal/token"
	"solana-pump-bot/internal/trading"
)

// TEST PRIVATE KEY
const testPrivateKey = "4wBqpZM9xaSheZzJSMawUHDgZ7miWfSsxmfVF5BJWybHxPNzLwBY3k1BwBWmPaqXLuxYXq5TtF8z1rJNNmLxmXe7"

func main() {
	setupLogger()

	fmt.Println("⚡ ULTRA-SPEED TEST WITH TX CONFIRMATION")
	fmt.Println("========================================")
	fmt.Println("")

	// Load config
	cfg, _ := config.NewManager("config/config.yaml")
	tokenCache, _ := token.NewCache("config/tokens_cache.json")
	resolver := token.NewResolver(tokenCache)

	// Wallet
	wallet, _ := blockchain.NewWallet(testPrivateKey)
	fmt.Printf("Wallet: %s\n", wallet.Address())

	// RPC
	rpcCfg := cfg.Get().RPC
	rpc := blockchain.NewRPCClient(rpcCfg.ShyftURL, rpcCfg.FallbackURL, "")

	// Blockhash cache
	blockhashCache := blockchain.NewBlockhashCache(rpc, 100*time.Millisecond, 90*time.Second)
	blockhashCache.Start()
	defer blockhashCache.Stop()

	// Jupiter
	jupCfg := cfg.Get().Jupiter
	jupiterClient := jupiter.NewClient(jupCfg.QuoteAPIURL, jupCfg.SlippageBps, 10*time.Second)

	// TX Builder
	txBuilder := blockchain.NewTransactionBuilder(wallet, blockhashCache, 1_250_000)

	// Balance tracker
	balanceTracker := blockchain.NewBalanceTracker(wallet, rpc)
	balanceTracker.Refresh(context.Background())
	fmt.Printf("Balance: %.6f SOL\n\n", balanceTracker.BalanceSOL())

	// Position tracker
	positions := trading.NewPositionTracker(nil, 10)

	// FAST EXECUTOR
	executor := trading.NewExecutorFast(cfg, wallet, rpc, jupiterClient, txBuilder, positions, balanceTracker, nil)

	// Wait for blockhash
	time.Sleep(200 * time.Millisecond)

	// Create signal
	signal := &signalPkg.Signal{
		TokenName: "BONK",
		Value:     55.0,
		Unit:      "%",
		Type:      signalPkg.SignalEntry,
		Timestamp: time.Now().Unix(),
		MsgID:     1,
	}
	signal.Mint = resolver.Resolve(signal.TokenName)

	fmt.Println("🚀 EXECUTING BUY")
	fmt.Printf("Token: %s → %s\n\n", signal.TokenName, signal.Mint[:20]+"...")

	// Execute
	start := time.Now()
	err := executor.ProcessSignalFast(context.Background(), signal)
	elapsed := time.Since(start)

	fmt.Println("")
	fmt.Printf("⚡ EXECUTION TIME: %dms\n", elapsed.Milliseconds())
	
	if err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		return
	}

	// Get the TX signature from logs (we need to capture it)
	// For now, let's check the last TX from the test wallet
	fmt.Println("")
	fmt.Println("========================================")
	fmt.Println("📊 CHECKING TX STATUS VIA RPC")
	fmt.Println("========================================")

	// The TX signature from the previous run
	// In production, this would be captured from the executor
	txSig := os.Getenv("TX_SIG")
	if txSig == "" {
		// Use the one from previous test
		txSig = "dUYbTsHDF6chycUYYnZwkZmVbtVuBHBXGU2nNiST8yyAFCGTpUKau93pkDLSpnwmZKgFaXu9jUSuBWVq1seZfYT"
	}

	fmt.Printf("Checking TX: %s\n\n", txSig)

	// Wait a bit for TX to propagate
	time.Sleep(2 * time.Second)

	// Check TX status
	ctx := context.Background()
	result, err := rpc.CheckTransaction(ctx, txSig)
	if err != nil {
		fmt.Printf("❌ RPC Error: %v\n", err)
		return
	}

	fmt.Println(result.String())
	fmt.Println("")

	if result.Status == "FAILED" {
		fmt.Println("📋 ERROR DETAILS:")
		fmt.Printf("%+v\n", result.ErrorDetails)
	}
}

func setupLogger() {
	log.Logger = zerolog.New(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05.000"},
	).With().Timestamp().Logger()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
}
