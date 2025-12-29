package main

import (
	"context"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	
	"solana-pump-bot/internal/blockchain"
	"solana-pump-bot/internal/config"
	"solana-pump-bot/internal/jupiter"
	signalPkg "solana-pump-bot/internal/signal"
	"solana-pump-bot/internal/trading"
)

func main() {
	// 1. Setup Logger
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"})
	zerolog.SetGlobalLevel(zerolog.InfoLevel) // Hide DBG logs (blockhash spam)
	log.Info().Msg("🚀 STARTING SIMULATION MODE 🚀")

	// 2. Load Config & Force Sim Mode
	cfg, err := config.NewManager("config/config.yaml")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}
	time.Sleep(500 * time.Millisecond) // Wait for initial file watcher reload to settle
	
	cfg.Update(func(c *config.Config) {
		c.Trading.SimulationMode = true
		c.Trading.AutoTradingEnabled = true
		c.Trading.MinEntryPercent = 50.0 // Ensure our signal triggers
		c.Trading.TakeProfitMultiple = 2.0
		// MaxAllocPercent to ensure calc works with Sim Balance (1:1)
		c.Trading.MaxAllocPercent = 100.0
	})

	// 3. Init Components
	// Use a dummy private key (Base58 compliant: Alphanumeric, no 0OIl)
	// 87 chars of 'a' (represents roughly 64 bytes)
	dummyKey := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if os.Getenv("WALLET_PRIVATE_KEY") == "" {
		os.Setenv("WALLET_PRIVATE_KEY", dummyKey) 
	}
	
	wallet, err := blockchain.NewWallet(os.Getenv("WALLET_PRIVATE_KEY"))
	if err != nil {
		log.Warn().Err(err).Msg("Wallet init failed. Trying fallback key.")
		// Fallback to strict valid key if above fake one fails len/format check
		// Proceeding anyway but this will crash if wallet is nil. 
		// Since we can't easily mock Wallet struct, we MUST succeed here.
		// If failure persists, it's a blocker.
	}
    
    if wallet == nil {
        log.Fatal().Msg("CRITICAL: Wallet is nil. Cannot create Executor.")
    }

	// Correct RPC Client init (3 args)
	rpc := blockchain.NewRPCClient(cfg.Get().RPC.ShyftURL, cfg.Get().RPC.FallbackURL, cfg.Get().RPC.ShyftAPIKeyEnv)
	
	jup := jupiter.NewClient(cfg.Get().Jupiter.QuoteAPIURL, 50, 5*time.Second)
	
	// Blockhash Cache
	blockhashCache := blockchain.NewBlockhashCache(rpc, 100*time.Millisecond, 30*time.Second)
	
	// Balance Tracker (Wallet, RPC)
	balance := blockchain.NewBalanceTracker(wallet, rpc)
	
	// Transaction Builder (Wallet, Cache, Fee)
	txBuilder := blockchain.NewTransactionBuilder(wallet, blockhashCache, 1000)

	tracker := trading.NewPositionTracker(nil, 100)
	
	// Init Executor
	executor := trading.NewExecutorFast(
		cfg,
		wallet,
		rpc,
		jup,
		txBuilder,
		tracker,
		balance,
		nil, // No DB
	)

	// 4. Start Monitoring (Mocked)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	blockhashCache.Start() // Start cache
	
	// Enable Jupiter Sim Mode: Price Multiplier 1.0 (Entry)
	jup.SetSimulation(true, 1.0)
	executor.SetSimulationMode(true) // Explicitly enable Sim Bypass
	
	executor.StartMonitoring(ctx)

	// 5. Test Step 1: Simulate BUY Signal (Up 50%)
	log.Info().Msg("--- STEP 1: SIGNAL TRIGGER (50% UP) ---")
	
	sig := &signalPkg.Signal{
		Mint:      "SimTokenMint123456789", // Fake Mint
		TokenName: "SIMTEST",
		Type:      signalPkg.SignalEntry,
		Value:     50.0,
		Unit:      "%",
		MsgID:     12345,
		Timestamp: time.Now().Unix(),
	}
	
	// ProcessSignalFast
	err = executor.ProcessSignalFast(ctx, sig)
	if err != nil {
		log.Error().Err(err).Msg("Buy signal processing failed")
	} else {
		log.Info().Msg("✅ Buy Signal Processed (Check logs for SIM_BUY)")
	}

	// 6. Test Step 2: Wait 10s
	log.Info().Msg("⏳ Waiting 10s to simulate market movement...")
	time.Sleep(10 * time.Second)

	// 7. Test Step 3: Simulate 2X Price Jump
	log.Info().Msg("--- STEP 2: PRICE JUMP (2.5X) ---")
	// Set Jup multiplier to 2.5X (should trigger >2X sell)
	jup.SetSimulation(true, 2.5)
	
	// Wait for monitor loop (runs every 5s)
	log.Info().Msg("⏳ Waiting for Monitor Loop to catch 2X...")
	time.Sleep(7 * time.Second) // Give it time to tick
	
	// Check if sold
	posCount := len(executor.GetOpenPositions())
	if posCount == 0 {
		log.Info().Msg("✅ SUCCESS! Position automatically sold.")
	} else {
		log.Error().Int("positions", posCount).Msg("❌ FAIL: Position still open. 2X logic missed?")
		openPos := executor.GetOpenPositions()
		for _, p := range openPos {
			log.Info().Str("mint", p.Mint).Float64("curVal", p.CurrentValue).Msg("Open Position State")
		}
	}

	log.Info().Msg("🏁 SIMULATION COMPLETE")
}
