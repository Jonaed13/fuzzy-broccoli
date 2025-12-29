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

// TEST PRIVATE KEY - Empty wallet, for testing only
const testPrivateKey = "4wBqpZM9xaSheZzJSMawUHDgZ7miWfSsxmfVF5BJWybHxPNzLwBY3k1BwBWmPaqXLuxYXq5TtF8z1rJNNmLxmXe7"

func main() {
	setupLogger()
	
	fmt.Println("🧪 TEST BOT - 1:1 Copy with Test Key")
	fmt.Println("=====================================")
	fmt.Println("This is an EXACT copy of your bot logic")
	fmt.Println("")

	// Load config (exact same as real bot)
	cfg, err := config.NewManager("config/config.yaml")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	// Load token cache (exact same)
	tokenCache, err := token.NewCache("config/tokens_cache.json")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load token cache")
	}
	resolver := token.NewResolver(tokenCache)

	// Use TEST private key instead of env
	wallet, err := blockchain.NewWallet(testPrivateKey)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load wallet")
	}
	fmt.Printf("Test Wallet: %s\n\n", wallet.Address())

	// Initialize RPC (exact same)
	rpcCfg := cfg.Get().RPC
	rpc := blockchain.NewRPCClient(rpcCfg.ShyftURL, rpcCfg.FallbackURL, cfg.GetShyftAPIKey())

	// Initialize blockhash cache (exact same)
	blockhashCache := blockchain.NewBlockhashCache(
		rpc,
		cfg.GetBlockhashRefresh(),
		time.Duration(cfg.Get().Blockchain.BlockhashTTLSeconds)*time.Second,
	)
	if err := blockhashCache.Start(); err != nil {
		log.Fatal().Err(err).Msg("failed to start blockhash cache")
	}
	defer blockhashCache.Stop()

	// Initialize Jupiter (exact same)
	jupCfg := cfg.Get().Jupiter
	jupiterClient := jupiter.NewClient(
		jupCfg.QuoteAPIURL,
		jupCfg.SlippageBps,
		time.Duration(jupCfg.TimeoutSeconds)*time.Second,
	)

	// Initialize TX builder (exact same)
	priorityFeeLamports := uint64(cfg.Get().Fees.StaticPriorityFeeSol * 1e9)
	txBuilder := blockchain.NewTransactionBuilder(wallet, blockhashCache, priorityFeeLamports)

	// Initialize balance tracker (exact same)
	balanceTracker := blockchain.NewBalanceTracker(wallet, rpc)
	balanceTracker.Refresh(context.Background())
	fmt.Printf("Balance: %.6f SOL\n\n", balanceTracker.BalanceSOL())

	// NO DB for test (skip SQLite)
	// Initialize position tracker (in-memory only)
	positions := trading.NewPositionTracker(nil, cfg.GetTrading().MaxOpenPositions)

	// Initialize executor (exact same as real bot, but no DB)
	executor := trading.NewExecutor(cfg, wallet, rpc, jupiterClient, txBuilder, positions, balanceTracker, nil)

	// --- SIMULATE A BUY SIGNAL ---
	fmt.Println("🚀 SIMULATING BUY SIGNAL")
	fmt.Println("========================")

	// Create fake signal (as if from Telegram)
	testSignal := &signalPkg.Signal{
		TokenName: "BONK",
		Value:     55.0,
		Unit:      "%",
		Type:      signalPkg.SignalEntry,
		Timestamp: time.Now().Unix(),
		MsgID:     999999,
	}

	// Resolve token (exact same as real bot)
	testSignal.Mint = resolver.Resolve(testSignal.TokenName)
	fmt.Printf("Token: %s\n", testSignal.TokenName)
	fmt.Printf("Mint: %s\n", testSignal.Mint)
	fmt.Printf("Signal: %.1f%s (Type: %s)\n\n", testSignal.Value, testSignal.Unit, testSignal.Type)

	// Execute trade (exact same as real bot)
	fmt.Println("⏱️  MEASURING EXECUTION TIME...")
	start := time.Now()

	err = executor.ProcessSignal(context.Background(), testSignal)

	elapsed := time.Since(start)

	fmt.Println("")
	fmt.Println("=====================================")
	if err != nil {
		fmt.Printf("❌ Trade failed: %v\n", err)
	} else {
		fmt.Println("✅ Trade processed (check logs for details)")
	}
	fmt.Printf("⏱️  TOTAL EXECUTION TIME: %dms\n", elapsed.Milliseconds())
	fmt.Println("=====================================")

	// Show position status
	openPositions := executor.GetOpenPositions()
	fmt.Printf("\nOpen positions: %d\n", len(openPositions))
	for _, pos := range openPositions {
		fmt.Printf("  - %s: %.4f SOL @ %.1f%s\n", pos.TokenName, pos.Size, pos.EntryValue, pos.EntryUnit)
	}
}

func setupLogger() {
	log.Logger = zerolog.New(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05.000"},
	).With().Timestamp().Logger()

	zerolog.SetGlobalLevel(zerolog.DebugLevel)
}
