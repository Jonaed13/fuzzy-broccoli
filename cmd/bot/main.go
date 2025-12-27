package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"solana-pump-bot/internal/blockchain"
	"solana-pump-bot/internal/config"
	"solana-pump-bot/internal/jupiter"
	"solana-pump-bot/internal/analytics"
	signalPkg "solana-pump-bot/internal/signal"
	"solana-pump-bot/internal/storage"
	"solana-pump-bot/internal/token"
	"solana-pump-bot/internal/trading"
	"solana-pump-bot/internal/tui"
)

func main() {
	// Check for TUI mode (default) or headless mode
	headless := os.Getenv("HEADLESS") == "1"

	if headless {
		runHeadless()
	} else {
		runWithTUI()
	}
}

func runHeadless() {
	setupLogger()
	log.Info().Msg("🚀 Solana Pump Bot starting (headless mode)...")

	// Initialize all components
	cfg, tokenResolver, signalChan, server, executor, balanceTracker, blockhashCache := initComponents()
	
	// Setup WebSocket for real-time updates
	if err := executor.SetupWebSocket(); err != nil {
		log.Warn().Err(err).Msg("WebSocket setup failed (will use polling)")
	}
	
	// Start monitor
	executor.StartMonitoring(context.Background())

	// Process signals
	go func() {
		for sig := range signalChan {
			if tokenResolver != nil && sig.Mint == "" {
				sig.Mint = tokenResolver.Resolve(sig.TokenName)
			}
			if executor != nil {
				executor.ProcessSignalFast(context.Background(), sig)
			}
		}
	}()

	// Start HTTP server
	go func() {
		if err := server.Start(); err != nil {
			log.Fatal().Err(err).Msg("signal server failed")
		}
	}()

	log.Info().
		Str("host", cfg.Get().Telegram.ListenHost).
		Int("port", cfg.Get().Telegram.ListenPort).
		Msg("signal server started")

	// Balance refresh loop
	if balanceTracker != nil {
		go func() {
			ticker := time.NewTicker(cfg.GetBalanceRefresh())
			defer ticker.Stop()
			for range ticker.C {
				balanceTracker.Refresh(context.Background())
			}
		}()
	}

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down...")
	server.Shutdown()
	if blockhashCache != nil {
		blockhashCache.Stop()
	}
	log.Info().Msg("goodbye 👋")
}

func runWithTUI() {
	// Redirect logs to file so they don't spam the TUI
	logFile, err := os.OpenFile("data/afnex.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Could not open log file: %v\n", err)
		logFile = nil
	}
	
	if logFile != nil {
		log.Logger = zerolog.New(logFile).With().Timestamp().Logger()
		zerolog.SetGlobalLevel(zerolog.InfoLevel) // Only info and above in TUI mode
	} else {
		// Fallback to discard logs
		log.Logger = zerolog.Nop()
	}

	// Initialize components
	cfg, tokenResolver, signalChan, server, executor, balanceTracker, blockhashCache := initComponents()

	// Setup WebSocket for real-time updates
	if err := executor.SetupWebSocket(); err != nil {
		log.Warn().Err(err).Msg("WebSocket setup failed (will use polling)")
	}

	// Create TUI model
	model := tui.NewModel(cfg)

	// Set callbacks
	db, _ := storage.NewDB("data/trades.db") // For export
	model.SetCallbacks(
		func() {
			// Toggle pause
			cfg.Update(func(c *config.Config) {
				c.Trading.AutoTradingEnabled = !c.Trading.AutoTradingEnabled
			})
		},
		func(mint string) {
			// Force close position
			if executor != nil {
				executor.ForceClose(context.Background(), mint)
			}
		},
		func() {
			// Clear stats and positions (F9) -> PANIC SELL ALL
			if executor != nil {
				executor.SellAllPositions(context.Background())
				// executor.ResetStats() // Optional: maybe don't reset stats on selling? User said clear positions.
				// Let's reset stats too as per "Clear" semantics usually implying reset.
				executor.ResetStats()
			}
		},
		func() {
			// Export trades to CSV (E key)
			if db != nil {
				path := fmt.Sprintf("trades_%s.csv", time.Now().Format("20060102_150405"))
				if err := analytics.ExportTradesToCSV(db, path); err != nil {
					log.Error().Err(err).Msg("CSV export failed")
				} else {
					log.Info().Str("path", path).Msg("Trades exported to CSV")
				}
			}
		},
	)

	// Create TUI program
	p := tea.NewProgram(model, tea.WithAltScreen())

	// Start HTTP server in background
	go func() {
		if err := server.Start(); err != nil {
			log.Error().Err(err).Msg("signal server failed")
		}
	}()

	// Process signals and update TUI
	go func() {
		for sig := range signalChan {
			if tokenResolver != nil && sig.Mint == "" {
				sig.Mint = tokenResolver.Resolve(sig.TokenName)
			}
			
			// Send to TUI
			tui.SendSignal(p, sig)
			
			// Execute trade (FAST - no blocking checks)
			if executor != nil {
				// FIX: Run async to not block signal ingestion
				go func(s *signalPkg.Signal) {
					executor.ProcessSignalFast(context.Background(), s)
					// Update positions in TUI
					tui.SendPositions(p, executor.GetOpenPositions())
				}(sig)
			}
		}
	}()

	// TUI Log Tailing (Fix for missing logs)
	go func() {
		file, err := os.Open("data/afnex.log")
		if err != nil {
			return
		}
		defer file.Close()

		// Seek to end initially to avoid spamming old logs
		file.Seek(0, 2)
		
		reader := bufio.NewReader(file)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				time.Sleep(100 * time.Millisecond) // Wait for new data
				continue
			}
			// Clean and send
			line = strings.TrimSpace(line)
			if line != "" {
				tui.SendLogs(p, []string{line})
			}
		}
	}()

	// Balance, latency, and stats refresh loop
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if balanceTracker != nil {
				start := time.Now()
				balanceTracker.Refresh(context.Background())
				latencyMs := time.Since(start).Milliseconds()
				tui.SendBalance(p, balanceTracker.BalanceSOL())
				tui.SendLatency(p, latencyMs)
			}
			if executor != nil {
				tui.SendPositions(p, executor.GetOpenPositions())
				// Send stats to TUI
				totalEntry, reached2X := executor.GetStats()
				tui.SendStats(p, totalEntry, reached2X)
			}
		}
	}()

	// Run TUI (blocking)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}

	// Cleanup
	server.Shutdown()
	if executor != nil {
		executor.Shutdown()
	}
	if blockhashCache != nil {
		blockhashCache.Stop()
	}
}

func initComponents() (
	*config.Manager,
	*token.Resolver,
	chan *signalPkg.Signal,
	*signalPkg.Server,
	*trading.ExecutorFast,
	*blockchain.BalanceTracker,
	*blockchain.BlockhashCache,
) {
	// Load config
	cfg, err := config.NewManager("config/config.yaml")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	// Load token cache
	tokenCache, err := token.NewCache("config/tokens_cache.json")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load token cache")
	}
	resolver := token.NewResolver(tokenCache)

	// Signal channel
	signalChan := make(chan *signalPkg.Signal, 100)

	// Create signal handler
	handler := signalPkg.NewHandler(
		signalChan,
		func() float64 { return cfg.GetTrading().MinEntryPercent },
		func() float64 { return cfg.GetTrading().TakeProfitMultiple },
		resolver.Resolve,
	)

	// Create HTTP server
	telegramCfg := cfg.Get().Telegram
	server := signalPkg.NewServer(telegramCfg.ListenHost, telegramCfg.ListenPort, handler)

	// Initialize blockchain components (only if wallet key is set)
	var wallet *blockchain.Wallet
	var rpc *blockchain.RPCClient
	var jupiterClient *jupiter.Client
	var txBuilder *blockchain.TransactionBuilder
	var balanceTracker *blockchain.BalanceTracker
	var blockhashCache *blockchain.BlockhashCache
	var executor *trading.ExecutorFast

	privateKey := cfg.GetPrivateKey()
	if privateKey != "" {
		// Load wallet from provided key
		wallet, err = blockchain.NewWallet(privateKey)
		if err != nil {
			log.Error().Err(err).Msg("failed to load wallet")
		}
	} else {
		// Use auto-generated cached wallet
		keyManager := blockchain.NewCachedKeyManager("./data", 10*time.Minute)
		wallet, err = keyManager.GetOrGenerate()
		if err != nil {
			log.Error().Err(err).Msg("failed to generate wallet")
		}
		log.Warn().Str("address", wallet.Address()).Msg("⚠️ USING AUTO-GENERATED WALLET - Fund this address to trade")
	}

	if wallet != nil {
		// Initialize RPC client
		rpcCfg := cfg.Get().RPC
		rpc = blockchain.NewRPCClient(rpcCfg.ShyftURL, rpcCfg.FallbackURL, cfg.GetShyftAPIKey())

		// Initialize blockhash cache
		blockhashCache = blockchain.NewBlockhashCache(
			rpc,
			cfg.GetBlockhashRefresh(),
			time.Duration(cfg.Get().Blockchain.BlockhashTTLSeconds)*time.Second,
		)
		if err := blockhashCache.Start(); err != nil {
			log.Error().Err(err).Msg("failed to start blockhash cache")
		}

		// Initialize Jupiter client
		jupCfg := cfg.Get().Jupiter
		jupiterClient = jupiter.NewClient(
			jupCfg.QuoteAPIURL,
			jupCfg.SlippageBps,
			time.Duration(jupCfg.TimeoutSeconds)*time.Second,
		)

		// Initialize transaction builder
		priorityFeeLamports := uint64(cfg.Get().Fees.StaticPriorityFeeSol * 1e9)
		txBuilder = blockchain.NewTransactionBuilder(wallet, blockhashCache, priorityFeeLamports)

		// Initialize balance tracker
		balanceTracker = blockchain.NewBalanceTracker(wallet, rpc)
		balanceTracker.Refresh(context.Background())
		
		// FIX: Show wallet address and balance at startup
		balanceSOL := balanceTracker.BalanceSOL()
		log.Info().
			Str("address", wallet.Address()).
			Float64("balance", balanceSOL).
			Msg("💰 WALLET STATUS")
		
		// FIX: LOUD WARNING if balance is 0
		if balanceSOL == 0 {
			log.Error().
				Str("address", wallet.Address()).
				Msg("⚠️⚠️⚠️ WALLET IS EMPTY! Bot cannot trade. Fund this address with SOL! ⚠️⚠️⚠️")
			fmt.Printf("\n\033[1;31m")
			fmt.Printf("╔════════════════════════════════════════════════════════════╗\n")
			fmt.Printf("║              ⚠️  WALLET HAS 0 SOL! ⚠️                        ║\n")
			fmt.Printf("╠════════════════════════════════════════════════════════════╣\n")
			fmt.Printf("║  Address: %-48s  ║\n", wallet.Address())
			fmt.Printf("║  Balance: 0.00 SOL                                        ║\n")
			fmt.Printf("║                                                            ║\n")
			fmt.Printf("║  → Send 0.1+ SOL to trade!                                 ║\n")
			fmt.Printf("╚════════════════════════════════════════════════════════════╝\n")
			fmt.Printf("\033[0m\n")
		} else if balanceSOL < 0.01 {
			log.Warn().
				Float64("balance", balanceSOL).
				Msg("⚠️ Low balance - may not cover trade + fees")
		}

		// Initialize storage
		db, err := storage.NewDB(cfg.Get().Storage.SQLitePath)
		if err != nil {
			log.Error().Err(err).Msg("failed to initialize database")
		}

		// Initialize position tracker
		positions := trading.NewPositionTracker(db, cfg.GetTrading().MaxOpenPositions)

		// Initialize FAST executor (no balance checks, no preflight, fire-and-forget)
		executor = trading.NewExecutorFast(cfg, wallet, rpc, jupiterClient, txBuilder, positions, balanceTracker, db)

		log.Info().
			Str("wallet", wallet.Address()).
			Float64("balance", balanceTracker.BalanceSOL()).
			Int("tokens", resolver.CacheSize()).
			Msg("trading engine initialized")
	}

	return cfg, resolver, signalChan, server, executor, balanceTracker, blockhashCache
}

func setupLogger() {
	log.Logger = zerolog.New(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"},
	).With().Timestamp().Logger()

	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if os.Getenv("DEBUG") == "1" {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
}
