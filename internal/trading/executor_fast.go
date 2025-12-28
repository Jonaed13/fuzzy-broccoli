package trading

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"solana-pump-bot/internal/blockchain"
	"solana-pump-bot/internal/config"
	"solana-pump-bot/internal/jupiter"
	signalPkg "solana-pump-bot/internal/signal"
	"solana-pump-bot/internal/storage"
	ws "solana-pump-bot/internal/websocket"

	"github.com/rs/zerolog/log"
)

// safeMintPrefix returns first 8 chars of mint or full mint if shorter
func safeMintPrefix(mint string) string {
	if len(mint) >= 8 {
		return mint[:8] + "..."
	}
	if len(mint) > 0 {
		return mint + "..."
	}
	return "(empty)"
}

// TrackedToken represents a token being monitored for 2X detection timing
type TrackedToken struct {
	Mint        string
	TokenName   string
	EntryTime   time.Time  // When 50% signal received
	Bot2XTime   *time.Time // When bot detected 2X via WebSocket (nil if not yet)
	TG2XTime    *time.Time // When TG announced 2X (nil if not yet)
	PoolAddr    string     // AMM pool address for WebSocket subscription
	Subscribed  bool       // Whether WebSocket subscription is active
	InitialMC   float64    // Virtual entry price (MC)
	TotalSupply float64    // Actual fetched supply
}

// ExecutorFast is the ultra-speed executor with NO BLOCKING CHECKS
// Philosophy: Send first, check later. Speed > Safety.
type ExecutorFast struct {
	cfg       *config.Manager
	wallet    *blockchain.Wallet
	rpc       *blockchain.RPCClient
	jupiter   *jupiter.Client
	txBuilder *blockchain.TransactionBuilder
	positions *PositionTracker
	balance   *blockchain.BalanceTracker
	db        *storage.DB
	metrics   *Metrics

	// Duplicate protection
	recentSignals map[int64]time.Time  // msgID -> timestamp
	recentMints   map[string]time.Time // mint -> last buy time
	mu            sync.RWMutex

	// Stats for TUI
	totalEntrySignals int
	reached2X         int
	seen2X            map[string]bool   // Track unique mints that hit 2X (prevent double count)
	seenEntry         map[string]bool   // Track mints we've seen Entry signals for (validate 2X)
	signalStatus      map[string]string // Track buy outcome per mint: "bought", "no_sol", "max_pos", "already_have", "rpc_error"
	statsMu           sync.RWMutex

	// 2X Detection Timer: Track all 50% signals for timing comparison
	trackedTokens   map[string]*TrackedToken
	trackedTokensMu sync.RWMutex

	// Retry config
	maxRetries int

	// Simulation Override
	simMode bool

	// WebSocket Real-Time
	wsClient  *ws.Client
	priceFeed *ws.PriceFeed
	walletMon *ws.WalletMonitor
	stopCh    chan struct{}

	// SOL Price Cache (updated via Jupiter)
	solPrice   float64
	solPriceMu sync.RWMutex
}

// NewExecutorFast creates an ultra-speed executor
func NewExecutorFast(
	cfg *config.Manager,
	wallet *blockchain.Wallet,
	rpc *blockchain.RPCClient,
	jupiterClient *jupiter.Client,
	txBuilder *blockchain.TransactionBuilder,
	positions *PositionTracker,
	balance *blockchain.BalanceTracker,
	db *storage.DB,
) *ExecutorFast {
	e := &ExecutorFast{
		cfg:           cfg,
		wallet:        wallet,
		rpc:           rpc,
		jupiter:       jupiterClient,
		txBuilder:     txBuilder,
		positions:     positions,
		balance:       balance,
		db:            db,
		metrics:       NewMetrics(),
		recentSignals: make(map[int64]time.Time, 1000),     // Pre-allocate for 1000 signals
		recentMints:   make(map[string]time.Time, 1000),    // Pre-allocate for 1000 mints
		seen2X:        make(map[string]bool, 100),          // Pre-allocate
		seenEntry:     make(map[string]bool, 500),          // Track valid entries for 2X validation
		signalStatus:  make(map[string]string, 500),        // Track buy outcomes for TUI
		trackedTokens: make(map[string]*TrackedToken, 500), // 2X timer tracking
		maxRetries:    2,
		stopCh:        make(chan struct{}), // FIX: Initialize stopCh in constructor
		solPrice:      185.0,               // Initial fallback until first fetch
	}

	// Phase 2: Start cleanup routine to prevent memory leaks
	e.startCleanupRoutine()

	// Phase 10: Start SOL price cache routine
	e.startSolPriceRoutine()

	// Phase 8: Load persisted stats
	if db != nil {
		stats := db.LoadAllBotStats()
		e.statsMu.Lock()
		e.totalEntrySignals = stats["total_entries"]
		e.reached2X = stats["reached_2x"]
		e.statsMu.Unlock()
		log.Info().
			Int("entries", e.totalEntrySignals).
			Int("wins", e.reached2X).
			Msg("Loaded persistence stats from DB")
	}

	// Load previously seen entries (last 24h) to validate exits
	if db != nil {
		if seen, err := db.LoadSeenEntries(); err == nil {
			e.statsMu.Lock()
			for k := range seen {
				e.seenEntry[k] = true
			}
			e.statsMu.Unlock()
			log.Info().Int("count", len(seen)).Msg("Restored seen entries from DB")
		}
	}

	return e
}

// SetSimulationMode overrides config simulation mode
func (e *ExecutorFast) SetSimulationMode(enabled bool) {
	e.simMode = enabled
	log.Info().Bool("enabled", enabled).Msg("ExecutorFast Simulation Mode Set")
}

// SetupWebSocket initializes WebSocket connection for real-time updates
func (e *ExecutorFast) SetupWebSocket() error {
	wsCfg := e.cfg.Get().WebSocket
	if wsCfg.ShyftURL == "" {
		log.Warn().Msg("WebSocket URL not configured, skipping real-time setup")
		return nil
	}

	reconnectDelay := time.Duration(wsCfg.ReconnectDelayMs) * time.Millisecond
	pingInterval := time.Duration(wsCfg.PingIntervalMs) * time.Millisecond

	e.wsClient = ws.NewClient(wsCfg.ShyftURL, reconnectDelay, pingInterval)
	// Note: stopCh is already initialized in NewExecutorFast

	// Set connection callbacks
	e.wsClient.SetCallbacks(
		func() {
			log.Info().Msg("📡 WebSocket connected - real-time mode active")
			// Re-subscribe to all tracked positions
			e.resubscribePositions()
		},
		func(err error) {
			log.Warn().Err(err).Msg("📡 WebSocket disconnected")
			// Phase 7: Clear subscription maps to prevent duplicates on reconnect
			if e.priceFeed != nil {
				e.priceFeed.ClearSubscriptions()
			}
		},
	)

	// Connect
	if err := e.wsClient.Connect(); err != nil {
		return fmt.Errorf("WebSocket connect: %w", err)
	}

	// Setup price feed
	walletAddr := ""
	if e.wallet != nil {
		walletAddr = e.wallet.Address()
	}
	e.priceFeed = ws.NewPriceFeed(e.wsClient, walletAddr)

	// Register price update handler
	e.priceFeed.OnPriceUpdate(func(update ws.PriceUpdate) {
		e.handleRealTimePriceUpdate(update)
	})

	// Setup wallet monitor for balance + TX confirmation
	e.walletMon = ws.NewWalletMonitor(e.wsClient, walletAddr)
	e.walletMon.OnBalanceUpdate(func(update ws.BalanceUpdate) {
		e.handleWalletBalanceUpdate(update)
	})

	// Start wallet balance subscription
	if err := e.walletMon.StartWalletSubscription(); err != nil {
		log.Warn().Err(err).Msg("failed to start wallet subscription")
	}

	urlDisplay := wsCfg.ShyftURL
	if len(urlDisplay) > 40 {
		urlDisplay = urlDisplay[:40] + "..."
	}
	log.Info().Str("url", urlDisplay).Msg("WebSocket + PriceFeed + WalletMonitor initialized")
	return nil
}

// Shutdown gracefully closes WebSocket connections (FIX #5)
func (e *ExecutorFast) Shutdown() {
	log.Info().Msg("Shutting down ExecutorFast...")

	// Close stop channel
	if e.stopCh != nil {
		close(e.stopCh)
	}

	// Close wallet monitor
	if e.walletMon != nil {
		e.walletMon.Stop()
	}

	// Close WebSocket client
	if e.wsClient != nil {
		e.wsClient.Close()
	}

	log.Info().Msg("ExecutorFast shutdown complete")
}

// startCleanupRoutine starts a background goroutine to clean up old map entries
// Phase 2: Memory leak fix
func (e *ExecutorFast) startCleanupRoutine() {
	ticker := time.NewTicker(5 * time.Minute)
	go func() {
		for {
			select {
			case <-ticker.C:
				e.cleanupOldEntries()
			case <-e.stopCh:
				ticker.Stop()
				return
			}
		}
	}()
	log.Debug().Msg("Cleanup routine started (5 min interval)")
}

// cleanupOldEntries removes old entries from maps to prevent memory leaks
func (e *ExecutorFast) cleanupOldEntries() {
	e.mu.Lock()
	defer e.mu.Unlock()

	cutoff := time.Now().Add(-1 * time.Hour)
	cleanedSignals := 0
	cleanedMints := 0

	// Clean recentSignals (older than 1 hour)
	for msgID, ts := range e.recentSignals {
		if ts.Before(cutoff) {
			delete(e.recentSignals, msgID)
			cleanedSignals++
		}
	}

	// Clean recentMints (older than 1 hour)
	for mint, ts := range e.recentMints {
		if ts.Before(cutoff) {
			delete(e.recentMints, mint)
			cleanedMints++
		}
	}

	// Clean signalStatus (keep only last 500)
	e.statsMu.Lock()
	if len(e.signalStatus) > 500 {
		// Reset if too many entries
		e.signalStatus = make(map[string]string)
		log.Info().Msg("signalStatus map reset (exceeded 500 entries)")
	}
	e.statsMu.Unlock()

	// Clean old tracked tokens (24 hour cutoff)
	e.cleanupOldTrackedTokens()

	if cleanedSignals > 0 || cleanedMints > 0 {
		log.Debug().
			Int("signals", cleanedSignals).
			Int("mints", cleanedMints).
			Msg("Cleaned old map entries")
	}
}

// resubscribePositions resubscribes to all tracked positions after reconnect
func (e *ExecutorFast) resubscribePositions() {
	if e.priceFeed == nil {
		return
	}
	for _, pos := range e.positions.GetAll() {
		if pos.PoolAddr == "" {
			continue // Skip positions without pool address
		}
		if err := e.priceFeed.TrackToken(pos.Mint, pos.PoolAddr); err != nil {
			log.Warn().Err(err).Str("mint", safeMintPrefix(pos.Mint)).Msg("failed to resubscribe")
		}
	}
}

// handleRealTimePriceUpdate processes WebSocket price updates for INSTANT 2X detection
func (e *ExecutorFast) handleRealTimePriceUpdate(update ws.PriceUpdate) {
	pos := e.positions.Get(update.Mint)
	if pos == nil {
		return
	}

	// Update position with new balance
	if update.TokenBalance == 0 && pos.TokenBalance > 0 {
		log.Warn().Str("mint", safeMintPrefix(update.Mint)).Msg("token balance dropped to 0 - removing position")
		e.positions.Remove(update.Mint)
		return
	}

	// Real-time price check (if price available from WebSocket)
	if update.PriceSOL > 0 {
		// Calculate current value
		currentValueSOL := update.PriceSOL * float64(update.TokenBalance)

		// THREAD-SAFE: Use UpdateStats to atomically update position
		multiple := pos.UpdateStats(currentValueSOL, update.TokenBalance)

		// INSTANT 2X CHECK (per ms, not per 5 seconds!)
		cfg := e.cfg.GetTrading()
		if cfg.AutoTradingEnabled && multiple >= cfg.TakeProfitMultiple && !pos.IsReached2X() {
			pos.SetReached2X(true)
			// FIX: Use increment2XSignal for stats (works even with 0 SOL)
			e.increment2XSignal(pos.Mint)

			// 2X Timer: Record when bot detected 2X via WebSocket
			e.setBotDetected2X(update.Mint)

			log.Info().
				Str("token", pos.TokenName).
				Float64("multiple", multiple).
				Msg("🚀 REAL-TIME 2X DETECTED - TRIGGERING AUTO-SELL")

			// Trigger sell immediately
			// FIX: Capture variables by value to prevent closure bugs
			mint := update.Mint
			tokenName := pos.TokenName
			mult := multiple
			go func() {
				signal := &signalPkg.Signal{
					Mint:      mint,
					TokenName: tokenName,
					Type:      signalPkg.SignalExit,
					Value:     mult,
					Unit:      "X",
				}
				e.executeSellFast(context.Background(), signal, NewTradeTimer())
			}()
		}

		log.Debug().
			Str("mint", safeMintPrefix(update.Mint)).
			Float64("price", update.PriceSOL).
			Float64("pnl", pos.PnLPercent).
			Msg("real-time price update")
	} else {
		// Just update balance if no price
		pos.SetTokenBalance(update.TokenBalance)
	}
}

// handleWalletBalanceUpdate processes real-time wallet SOL balance changes
func (e *ExecutorFast) handleWalletBalanceUpdate(update ws.BalanceUpdate) {
	// Update balance tracker with new value
	if e.balance != nil {
		e.balance.SetBalance(update.Lamports)
	}

	log.Debug().
		Float64("sol", float64(update.Lamports)/1e9).
		Uint64("slot", update.Slot).
		Msg("real-time wallet balance update")
}

// ProcessSignalFast processes signal with ZERO blocking checks
// NO balance check, NO position check, NO waiting - just send
func (e *ExecutorFast) ProcessSignalFast(ctx context.Context, signal *signalPkg.Signal) error {
	timer := NewTradeTimer()

	if signal.Mint == "" {
		return nil
	}

	// FIX #4: Duplicate signal protection
	if e.isDuplicateSignal(signal.MsgID) {
		log.Debug().Int64("msgID", signal.MsgID).Msg("duplicate signal ignored")
		e.setSignalStatus(signal.Mint, "duplicate")
		return nil
	}
	e.markSignalSeen(signal.MsgID)

	timer.MarkParseDone()
	timer.MarkResolveDone()

	// COUNT SIGNALS FIRST (regardless of trading enabled)
	// This ensures stats update even when auto-trading is off
	switch signal.Type {
	case signalPkg.SignalEntry:
		e.incrementEntrySignals()
		// Track this mint as having a valid entry (for 2X validation)
		e.statsMu.Lock()
		e.seenEntry[signal.Mint] = true
		e.statsMu.Unlock()

		// 2X Timer: Start tracking this token (regardless of buy success)
		// Enable tracking with parsed Initial MC for unbought tokens
		e.trackTokenFor2X(signal.Mint, signal.TokenName, signal.InitialMC)

		// Start async pool lookup and subscription for 2X detection
		go e.subscribeToPoolFor2X(signal.Mint)

		log.Info().
			Str("token", signal.TokenName).
			Float64("value", signal.Value).
			Str("unit", signal.Unit).
			Msg("📊 SIGNAL FOUND")
	case signalPkg.SignalExit:
		// FIX: Only count 2X if we've seen an Entry for this token
		// This prevents 2X count from exceeding Entry count
		e.statsMu.RLock()
		hasEntry := e.seenEntry[signal.Mint]
		e.statsMu.RUnlock()

		if hasEntry {
			e.increment2XSignal(signal.Mint)
		} else {
			// Log missing entry for clarity
			log.Warn().
				Str("token", signal.TokenName).
				Msg("⚠️ IGNORED 2X: Missed Entry Signal")
		}

		// 2X Timer: Record when TG announced 2X
		e.setTGAnnounced2X(signal.Mint)

		log.Info().
			Str("token", signal.TokenName).
			Float64("value", signal.Value).
			Bool("counted", hasEntry).
			Msg("📊 2X SIGNAL DETECTED")
	}

	// Check if trading enabled (only for execution, not counting)
	if !e.cfg.GetTrading().AutoTradingEnabled {
		e.setSignalStatus(signal.Mint, "monitoring")
		return nil
	}

	// Execute trades
	switch signal.Type {
	case signalPkg.SignalEntry:
		return e.executeBuyFast(ctx, signal, timer)
	case signalPkg.SignalExit:
		if e.hasMintPosition(signal.Mint) {
			return e.executeSellFast(ctx, signal, timer)
		}
	}
	return nil
}

// executeBuyFast - FIRE AND FORGET buy execution with retry
// Constants for trade limits (configurable via config in future)
const (
	MinTradeLamports   = 5_000_000 // 0.005 SOL minimum for trade + fees
	MinAllocLamports   = 1_000_000 // 0.001 SOL minimum allocation
	PendingPositionTTL = 2 * time.Minute
	FailedPositionTTL  = 1 * time.Minute
	DuplicateSignalTTL = 5 * time.Minute
	SignalCleanupTTL   = 10 * time.Minute
)

func (e *ExecutorFast) executeBuyFast(ctx context.Context, signal *signalPkg.Signal, timer *TradeTimer) error {
	// Check if we can open more positions (enforce max_open_positions)
	if !e.positions.CanOpen() {
		e.setSignalStatus(signal.Mint, "max_pos")
		log.Warn().
			Str("token", signal.TokenName).
			Int("current", e.positions.Count()).
			Msg("❌ MAX POSITIONS REACHED - skipping buy")
		return fmt.Errorf("max open positions reached")
	}

	// Check if we already have this position
	if e.hasMintPosition(signal.Mint) {
		e.setSignalStatus(signal.Mint, "already_have")
		// Update CurrentValue and PnL even if we don't buy
		pos := e.positions.Get(signal.Mint)
		if pos != nil {
			var val float64 = signal.Value
			if signal.Unit == "X" {
				val = signal.Value * 100 // Convert 2.0X -> 200%
			}

			pos.CurrentValue = val
			if pos.EntryValue > 0 {
				pos.PnLPercent = ((pos.CurrentValue / pos.EntryValue) - 1) * 100
			}
			// Update DB
			e.positions.Add(pos)
		}

		log.Warn().Str("mint", signal.Mint).Msg("already have position, updated stats, skipping buy")
		return nil
	}

	cfg := e.cfg.GetTrading()

	// Calculate amount based on cached balance (NO RPC CALL)
	balanceLamports := e.balance.BalanceLamports()
	if e.simMode || e.cfg.Get().Trading.SimulationMode {
		balanceLamports = 1_000_000_000 // 1 SOL
	}

	// FIX: FAIL LOUDLY if balance is 0
	if balanceLamports == 0 {
		e.setSignalStatus(signal.Mint, "no_sol")
		log.Error().
			Str("token", signal.TokenName).
			Msg("❌ CANNOT BUY: Wallet balance is 0 SOL! Fund your wallet.")
		return fmt.Errorf("wallet balance is 0 - fund your wallet to trade")
	}

	// FAIL LOUDLY if balance is too low for minimum trade
	if balanceLamports < MinTradeLamports {
		e.setSignalStatus(signal.Mint, "low_sol")
		log.Error().
			Str("token", signal.TokenName).
			Float64("balanceSOL", float64(balanceLamports)/1e9).
			Float64("minRequired", float64(MinTradeLamports)/1e9).
			Msg("❌ CANNOT BUY: Balance too low for trade + fees")
		return fmt.Errorf("balance %.4f SOL too low (need %.4f)", float64(balanceLamports)/1e9, float64(MinTradeLamports)/1e9)
	}

	allocLamports := uint64(float64(balanceLamports) * cfg.MaxAllocPercent / 100)

	// Minimum allocation per trade
	if allocLamports < MinAllocLamports {
		allocLamports = MinAllocLamports
	}

	log.Info().
		Str("token", signal.TokenName).
		Str("mint", signal.Mint).
		Uint64("amount", allocLamports).
		Float64("balanceSOL", float64(balanceLamports)/1e9).
		Msg("⚡ FAST BUY - executing")

	// FIX: Race condition - Add pending position IMMEDIATELY to block other signals
	pendingPos := &Position{
		Mint:         signal.Mint,
		TokenName:    signal.TokenName,
		Size:         float64(allocLamports) / 1e9,
		EntryValue:   signal.Value,
		EntryUnit:    signal.Unit,
		EntryTime:    time.Now(),
		MsgID:        signal.MsgID,
		CurrentValue: signal.Value,
		PnLPercent:   0,
		EntryTxSig:   "PENDING",
	}
	e.positions.Add(pendingPos)

	// FIX #11: Retry logic with EXPONENTIAL BACKOFF
	var lastErr error
	for attempt := 0; attempt <= e.maxRetries; attempt++ {
		if attempt > 0 {
			// Phase 4: Set retry status for TUI visibility
			e.setSignalStatus(signal.Mint, fmt.Sprintf("retry_%d", attempt))

			backoffMs := 100 * (1 << (attempt - 1)) // 100ms, 200ms, 400ms, 800ms...
			log.Warn().Int("attempt", attempt+1).Int("backoffMs", backoffMs).Msg("retrying buy...")
			time.Sleep(time.Duration(backoffMs) * time.Millisecond)
		}

		// Simulation Mode Bypass
		if e.simMode || e.cfg.Get().Trading.SimulationMode {
			log.Warn().Msg("SIMULATION MODE: Skipping real transaction steps")
			timer.MarkQuoteDone()
			timer.MarkSignDone()
			timer.MarkSendDone()
			// Mock Success
			txSig := "SIM_BUY_" + signal.TokenName
			e.metrics.RecordTrade(true, 0, 0, 0, 0, 0)
			log.Info().Str("txSig", txSig).Msg("⚡ SIMULATION BUY EXECUTED")
			go e.trackPositionAsync(signal, allocLamports, txSig)
			return nil
		}

		// Get swap TX from Jupiter
		swapTx, err := e.jupiter.GetSwapTransaction(ctx, jupiter.SOLMint, signal.Mint, e.wallet.Address(), allocLamports)
		if err != nil {
			log.Error().Str("error", blockchain.HumanErrorWithAction(err)).Msg("⚡ JUPITER FAILED")
			// Phase 3: Set RPC error status
			e.setSignalStatus(signal.Mint, "rpc_error")
			lastErr = err
			continue
		}
		timer.MarkQuoteDone()

		// Sign TX
		signedTx, err := e.txBuilder.SignSerializedTransaction(swapTx)
		if err != nil {
			log.Error().Str("error", blockchain.HumanError(err)).Msg("⚡ SIGN FAILED")
			lastErr = err
			continue
		}
		timer.MarkSignDone()

		// SEND - skipPreflight = true for max speed
		txSig, err := e.rpc.SendTransaction(ctx, signedTx, true)
		timer.MarkSendDone()

		// Log metrics
		parse, resolve, quote, sign, send := timer.GetBreakdown()
		e.metrics.RecordTrade(err == nil, parse, resolve, quote, sign, send)

		if err != nil {
			log.Error().Str("error", blockchain.HumanErrorWithAction(err)).Msg("⚡ TX SEND FAILED")
			lastErr = err
			continue
		}

		log.Info().
			Str("txSig", txSig).
			Int64("totalMs", timer.TotalMs()).
			Int64("jupiterMs", quote).
			Int64("signMs", sign).
			Int64("sendMs", send).
			Msg("⚡ BUY SENT")

		// Mark as bought
		e.setSignalStatus(signal.Mint, "bought")

		// WebSocket TX Confirmation (instant feedback)
		if e.walletMon != nil {
			e.walletMon.WaitForConfirmation(txSig, func(conf ws.TxConfirmation) {
				if conf.Confirmed {
					log.Info().Str("sig", txSig[:12]+"...").Msg("✅ BUY CONFIRMED via WebSocket")
				} else {
					log.Error().Str("sig", txSig[:12]+"...").Str("err", conf.Error).Msg("❌ BUY FAILED via WebSocket")
					// Remove failed position
					e.positions.Remove(signal.Mint)
				}
			})
		}

		// Track position ASYNC (don't block) - FIX #12: Use sync.WaitGroup for cleanup
		go e.trackPositionAsync(signal, allocLamports, txSig)

		return nil // Success
	}

	// Failed after retries - remove pending position
	e.positions.Remove(signal.Mint)
	if lastErr != nil {
		e.setSignalStatus(signal.Mint, "failed")
	}
	return lastErr
}

// executeSellFast - FIRE AND FORGET sell execution with retry
func (e *ExecutorFast) executeSellFast(ctx context.Context, signal *signalPkg.Signal, timer *TradeTimer) error {
	// Update position value for TUI display before selling
	if pos := e.positions.Get(signal.Mint); pos != nil {
		// THREAD-SAFE: Use SetStatsFromSignal to update position stats
		pos.SetStatsFromSignal(signal.Value, signal.Unit)

		// Mark position as reached 2X (for TUI display) - counting is done in ProcessSignalFast
		if !pos.IsReached2X() {
			pos.SetReached2X(true)
			// NOTE: Stats counting is now done in ProcessSignalFast.increment2XSignal()
			// to ensure it works even with 0 SOL. Don't double count here.
		}
		e.positions.Add(pos) // Update DB
	}

	// FIX #2 & #6: Get actual token balance
	// OPTIMIZATION: Use cached balance from WebSocket if available and fresh (within 1s)
	var tokenAmount uint64
	var err error

	if pos := e.positions.Get(signal.Mint); pos != nil && pos.TokenBalance > 0 && time.Since(pos.LastUpdate) < 1*time.Second {
		tokenAmount = pos.TokenBalance
		log.Debug().Uint64("amount", tokenAmount).Msg("using cached token balance for sell")
	} else {
		tokenAmount, err = e.getTokenBalance(ctx, signal.Mint)
		if err != nil {
			log.Error().Err(err).Msg("failed to get token balance for sell")
			return err
		}
	}

	if tokenAmount == 0 {
		log.Warn().Str("mint", signal.Mint).Msg("no token balance to sell")
		return nil
	}

	log.Info().
		Str("token", signal.TokenName).
		Str("mint", signal.Mint).
		Uint64("amount", tokenAmount).
		Msg("⚡ FAST SELL")

	// FIX #11: Retry logic with EXPONENTIAL BACKOFF
	var lastErr error

	// Simulation Bypass (Sell)
	if e.simMode || e.cfg.Get().Trading.SimulationMode {
		log.Warn().Msg("SIMULATION MODE: Skipping real SELL transaction")
		// Simulate latency
		timer.MarkQuoteDone()
		timer.MarkSignDone()
		timer.MarkSendDone()
		txSig := "SIM_SELL_" + signal.TokenName
		e.removePositionAsync(signal.Mint)
		log.Info().Str("txSig", txSig).Msg("⚡ SIMULATION SELL EXECUTED")
		return nil
	}
	for attempt := 0; attempt <= e.maxRetries; attempt++ {
		if attempt > 0 {
			backoffMs := 100 * (1 << (attempt - 1)) // 100ms, 200ms, 400ms, 800ms...
			log.Warn().Int("attempt", attempt+1).Int("backoffMs", backoffMs).Msg("retrying sell...")
			time.Sleep(time.Duration(backoffMs) * time.Millisecond)
		}

		// Get swap TX
		swapTx, err := e.jupiter.GetSwapTransaction(ctx, signal.Mint, jupiter.SOLMint, e.wallet.Address(), tokenAmount)
		if err != nil {
			log.Error().Str("error", blockchain.HumanErrorWithAction(err)).Msg("⚡ JUPITER FAILED")
			lastErr = err
			continue
		}
		timer.MarkQuoteDone()

		// Sign
		signedTx, err := e.txBuilder.SignSerializedTransaction(swapTx)
		if err != nil {
			log.Error().Str("error", blockchain.HumanError(err)).Msg("⚡ SIGN FAILED")
			lastErr = err
			continue
		}
		timer.MarkSignDone()

		// SEND
		txSig, err := e.rpc.SendTransaction(ctx, signedTx, true)
		timer.MarkSendDone()

		parse, resolve, quote, sign, send := timer.GetBreakdown()
		e.metrics.RecordTrade(err == nil, parse, resolve, quote, sign, send)

		if err != nil {
			log.Error().Str("error", blockchain.HumanErrorWithAction(err)).Msg("⚡ TX SEND FAILED")
			lastErr = err
			continue
		}

		log.Info().
			Str("txSig", txSig).
			Int64("totalMs", timer.TotalMs()).
			Msg("⚡ SELL SENT")

		// Log SELL trade to history
		if pos := e.positions.Get(signal.Mint); pos != nil && e.db != nil {
			duration := time.Since(pos.EntryTime).Seconds()
			e.db.InsertTrade(&storage.Trade{
				Mint:       signal.Mint,
				TokenName:  signal.TokenName,
				Side:       "SELL",
				AmountSol:  pos.Size,
				EntryValue: pos.EntryValue,
				ExitValue:  pos.CurrentValue,
				PnL:        pos.PnLPercent,
				Duration:   int64(duration),
				EntryTxSig: pos.EntryTxSig,
				ExitTxSig:  txSig,
				Timestamp:  time.Now().Unix(),
			})
		}

		// WebSocket TX Confirmation for sell
		if e.walletMon != nil {
			mintCopy := signal.Mint // Capture for closure
			e.walletMon.WaitForConfirmation(txSig, func(conf ws.TxConfirmation) {
				if conf.Confirmed {
					log.Info().Str("sig", txSig[:12]+"...").Msg("✅ SELL CONFIRMED via WebSocket")
					// Remove position only after confirmed
					e.positions.Remove(mintCopy)
				} else {
					log.Error().Str("sig", txSig[:12]+"...").Str("err", conf.Error).Msg("❌ SELL FAILED via WebSocket")
				}
			})
		} else {
			// Remove position ASYNC - FIX #12
			go e.removePositionAsync(signal.Mint)
		}

		return nil // Success
	}

	return lastErr
}

// FIX #2: Get actual token balance
func (e *ExecutorFast) getTokenBalance(ctx context.Context, mint string) (uint64, error) {
	if e.simMode || e.cfg.Get().Trading.SimulationMode {
		// Return 1,000,000 "lamports" or whatever units (Token Decimals?)
		// To match Jupiter expectations, we should probably check what Jupiter expects.
		// For monitoring, we just need > 0.
		// Let's assume 1000 tokens * 1e6 decimals = 1_000_000_000
		return 1_000_000_000, nil
	}
	// Get token accounts for this mint
	tokenAccounts, err := e.rpc.GetTokenAccountsByOwner(ctx, e.wallet.Address(), mint)
	if err != nil {
		return 0, err
	}

	var totalBalance uint64
	for _, acc := range tokenAccounts {
		totalBalance += acc.Amount
	}

	return totalBalance, nil
}

// FIX #4: Duplicate signal protection
func (e *ExecutorFast) isDuplicateSignal(msgID int64) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if ts, ok := e.recentSignals[msgID]; ok {
		// Consider duplicate if seen within DuplicateSignalTTL
		return time.Since(ts) < DuplicateSignalTTL
	}
	return false
}

func (e *ExecutorFast) markSignalSeen(msgID int64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.recentSignals[msgID] = time.Now()

	// Cleanup old entries from recentSignals
	for id, ts := range e.recentSignals {
		if time.Since(ts) > SignalCleanupTTL {
			delete(e.recentSignals, id)
		}
	}

	// Cleanup old entries from recentMints (was never cleaned - memory leak)
	for mint, ts := range e.recentMints {
		if time.Since(ts) > SignalCleanupTTL {
			delete(e.recentMints, mint)
		}
	}

	// Cleanup old entries from seen2X map (memory leak fix)
	e.statsMu.Lock()
	// seen2X tracks unique mints - clean entries older than 24h
	// Note: This requires tracking timestamps, so we clear periodically
	if len(e.seen2X) > 1000 {
		e.seen2X = make(map[string]bool) // Reset if too large
		log.Debug().Msg("cleared seen2X map (size exceeded 1000)")
	}
	e.statsMu.Unlock()
}

// FIX #2: Check if we already have a position - O(1) lookup using map
func (e *ExecutorFast) hasMintPosition(mint string) bool {
	return e.positions.Get(mint) != nil
}

// FIX #12: Async position tracking with proper context
func (e *ExecutorFast) trackPositionAsync(signal *signalPkg.Signal, allocLamports uint64, txSig string) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Msg("panic in trackPositionAsync")
		}
	}()

	pos := &Position{
		Mint:         signal.Mint,
		TokenName:    signal.TokenName,
		Size:         float64(allocLamports) / 1e9,
		EntryValue:   signal.Value,
		EntryUnit:    signal.Unit,
		EntryTime:    time.Now(),
		EntryTxSig:   txSig,
		MsgID:        signal.MsgID,
		CurrentValue: signal.Value, // Initialize to entry value
		PnLPercent:   0,            // Start at 0% PnL
	}
	e.positions.Add(pos)
	e.balance.Refresh(context.Background())

	// Log BUY trade to history
	if e.db != nil {
		e.db.InsertTrade(&storage.Trade{
			Mint:       signal.Mint,
			TokenName:  signal.TokenName,
			Side:       "BUY",
			AmountSol:  float64(allocLamports) / 1e9,
			EntryValue: signal.Value,
			ExitValue:  0,
			PnL:        0,
			Duration:   0,
			EntryTxSig: txSig,
			ExitTxSig:  "",
			Timestamp:  time.Now().Unix(),
		})
	}
}

// FIX #12: Async position removal with proper context
func (e *ExecutorFast) removePositionAsync(mint string) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Msg("panic in removePositionAsync")
		}
	}()

	e.positions.Remove(mint)
	e.balance.Refresh(context.Background())
}

// FIX #3: Stats tracking for TUI
func (e *ExecutorFast) incrementEntrySignals() {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	e.totalEntrySignals++
	if e.db != nil {
		go e.db.SaveBotStat("total_entries", e.totalEntrySignals)
	}
}

func (e *ExecutorFast) Increment2XHit() {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	e.reached2X++
	if e.db != nil {
		go e.db.SaveBotStat("reached_2x", e.reached2X)
	}
}

// increment2XSignal counts a 2X signal, but only once per mint (prevents double counting)
func (e *ExecutorFast) increment2XSignal(mint string) {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()

	// Only count each mint once
	if e.seen2X[mint] {
		return
	}
	e.seen2X[mint] = true
	e.reached2X++

	if e.db != nil {
		go e.db.SaveBotStat("reached_2x", e.reached2X)
	}
}

func (e *ExecutorFast) GetStats() (totalEntry int, reached2X int) {
	e.statsMu.RLock()
	defer e.statsMu.RUnlock()
	return e.totalEntrySignals, e.reached2X
}

// ResetStats clears the stats counters (called by F9)
func (e *ExecutorFast) ResetStats() {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	e.totalEntrySignals = 0
	e.reached2X = 0
	e.seenEntry = make(map[string]bool)      // Clear entry tracking
	e.seen2X = make(map[string]bool)         // Clear 2X tracking
	e.signalStatus = make(map[string]string) // Clear status tracking
}

// setSignalStatus records the buy outcome for a mint (thread-safe)
func (e *ExecutorFast) setSignalStatus(mint, status string) {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	e.signalStatus[mint] = status
}

// GetSignalStatus returns the buy outcome for a mint (for TUI display)
func (e *ExecutorFast) GetSignalStatus(mint string) string {
	e.statsMu.RLock()
	defer e.statsMu.RUnlock()
	return e.signalStatus[mint]
}

// GetAllSignalStatuses returns a copy of all signal statuses (for TUI)
func (e *ExecutorFast) GetAllSignalStatuses() map[string]string {
	e.statsMu.RLock()
	defer e.statsMu.RUnlock()
	result := make(map[string]string, len(e.signalStatus))
	for k, v := range e.signalStatus {
		result[k] = v
	}
	return result
}

// GetMetrics returns the metrics tracker
func (e *ExecutorFast) GetMetrics() *Metrics {
	return e.metrics
}

// SellAllPositions triggers a ForceClose for every active position
func (e *ExecutorFast) SellAllPositions(ctx context.Context) {
	log.Warn().Int("count", len(e.positions.GetAll())).Msg("🚨 PANIC SELL TRIGGERED: Selling ALL positions")

	positions := e.positions.GetAll()
	for _, pos := range positions {
		go func(mint string) {
			if err := e.ForceClose(ctx, mint); err != nil {
				log.Error().Err(err).Str("mint", mint).Msg("failed to force close during panic sell")
			}
		}(pos.Mint)
		// Small stagger to avoid rate limits
		time.Sleep(100 * time.Millisecond)
	}
}

// ForceClose force-closes a position by selling all tokens
func (e *ExecutorFast) ForceClose(ctx context.Context, mint string) error {
	timer := NewTradeTimer()

	signal := &signalPkg.Signal{
		Mint:      mint,
		TokenName: "FORCE_CLOSE",
		Type:      signalPkg.SignalExit,
	}

	return e.executeSellFast(ctx, signal, timer)
}

// StartMonitoring starts the background active trade monitor
func (e *ExecutorFast) StartMonitoring(ctx context.Context) {
	log.Info().Msg("starting active trade monitor (FAST mode)...")
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.monitorPositions(ctx)
			}
		}
	}()
}

func (e *ExecutorFast) monitorPositions(ctx context.Context) {
	positions := e.positions.GetAll()
	if len(positions) == 0 {
		return
	}

	cfg := e.cfg.GetTrading()

	for _, pos := range positions {
		// FIX: Handle stale PENDING positions (failed buys)
		if pos.EntryTxSig == "PENDING" {
			// If pending for more than PendingPositionTTL, mark as failed
			if time.Since(pos.EntryTime) > PendingPositionTTL {
				log.Warn().
					Str("token", pos.TokenName).
					Dur("age", time.Since(pos.EntryTime)).
					Msg("removing stale PENDING position (buy likely failed)")
				e.positions.Remove(pos.Mint)
				continue
			}
		}

		// Get current token balance
		balance, err := e.getTokenBalance(ctx, pos.Mint)

		// FIX: Handle 0 balance - mark position as lost/failed
		if err != nil {
			log.Debug().Err(err).Str("mint", safeMintPrefix(pos.Mint)).Msg("failed to get balance")
			continue
		}

		if balance == 0 {
			// Position has 0 tokens - either sold externally or buy failed
			if pos.EntryTxSig != "PENDING" && pos.EntryTxSig != "FAILED" {
				// Was a real position that now has 0 tokens
				log.Warn().
					Str("token", pos.TokenName).
					Msg("position has 0 tokens - marking as sold/failed")
				pos.CurrentValue = 0
				pos.PnLPercent = -100 // Show as total loss
				pos.EntryTxSig = "FAILED"
				// Keep it visible for FailedPositionTTL then remove
				if time.Since(pos.EntryTime) > FailedPositionTTL {
					e.positions.Remove(pos.Mint)
				}
			}
			continue
		}

		// Get Quote for ALL tokens -> SOL
		quote, err := e.jupiter.GetQuote(ctx, pos.Mint, jupiter.SOLMint, balance)
		if err != nil {
			continue
		}

		outAmount, _ := strconv.ParseUint(quote.OutAmount, 10, 64)
		currentValSOL := float64(outAmount) / 1e9

		// Update Position Stats
		pos.CurrentValue = currentValSOL
		pos.PnLSol = currentValSOL - pos.Size
		if pos.Size > 0 {
			pos.PnLPercent = ((currentValSOL / pos.Size) - 1.0) * 100
		}

		// Logic: 2X Detection
		multiple := 0.0
		if pos.Size > 0 {
			multiple = currentValSOL / pos.Size
		}

		if multiple >= cfg.TakeProfitMultiple { // Use config multiple (e.g. 2.0)
			if !pos.Reached2X {
				pos.Reached2X = true
				// Update DB to prevent double counting on restart
				e.positions.Add(pos)

				log.Info().Str("token", pos.TokenName).Float64("mult", multiple).Msg("reached target! marked as win")
				// Use increment2XSignal to prevent double counting with Telegram 2X signals
				e.increment2XSignal(pos.Mint)
			}

			// Trigger Auto-Sell
			// Trigger Auto-Sell
			if cfg.AutoTradingEnabled {
				if pos.Selling {
					// Already selling, skip to prevent dupes
					return
				}

				log.Info().Str("token", pos.TokenName).Msg("triggering take-profit sell")
				pos.Selling = true

				// Create timer
				timer := NewTradeTimer()

				// Create Exit Signal
				exitSig := &signalPkg.Signal{
					Mint:      pos.Mint,
					TokenName: pos.TokenName,
					Type:      signalPkg.SignalExit,
					Value:     multiple,
				}

				// Execute Sell
				go func() {
					e.executeSellFast(ctx, exitSig, timer)
					pos.Selling = false // Reset flag if sell failed and position remains
				}()
			}
		}

		// Logic: Partial Profit-Taking
		if cfg.PartialProfitPercent > 0 && cfg.PartialProfitMultiple > 1.0 {
			if multiple >= cfg.PartialProfitMultiple && !pos.PartialSold {
				log.Info().Str("token", pos.TokenName).Float64("mult", multiple).Msg("triggering partial profit take")
				e.executePartialSell(ctx, pos, cfg.PartialProfitPercent)
			}
		}

		// Logic: Time-Based Exit
		if cfg.MaxHoldMinutes > 0 {
			if time.Since(pos.EntryTime) > time.Duration(cfg.MaxHoldMinutes)*time.Minute {
				if pos.Selling {
					return
				}
				pos.Selling = true

				log.Info().Str("token", pos.TokenName).Msg("max hold time reached, selling all")
				// Create a fake signal to trigger full sell
				sig := &signalPkg.Signal{
					Mint:      pos.Mint,
					TokenName: pos.TokenName,
					Type:      signalPkg.SignalExit,
					Value:     currentValSOL,
				}
				go func() {
					e.executeSellFast(ctx, sig, NewTradeTimer())
					pos.Selling = false
				}()
			}
		}
	}
}

func (e *ExecutorFast) executePartialSell(ctx context.Context, pos *Position, percent float64) {
	// 1. Calculate Amount
	balance, err := e.getTokenBalance(ctx, pos.Mint)
	if err != nil {
		return
	}

	sellAmount := uint64(float64(balance) * (percent / 100.0))

	log.Info().Str("token", pos.TokenName).Msgf("selling %.0f%% of position...", percent)

	// 2. Perform Swap (Token -> SOL)
	swapTx, err := e.jupiter.GetSwapTransaction(ctx, pos.Mint, jupiter.SOLMint, e.wallet.Address(), sellAmount)
	if err != nil {
		log.Error().Err(err).Msg("failed partial swap tx")
		return
	}

	signedTx, err := e.txBuilder.SignSerializedTransaction(swapTx)
	if err != nil {
		return
	}

	txSig, err := e.rpc.SendTransaction(ctx, signedTx, true)
	if err != nil {
		log.Error().Err(err).Msg("failed partial sell send")
		return
	}

	// 3. Update Position State
	pos.PartialSold = true
	log.Info().Str("txSig", txSig).Msg("PARTIAL SELL executed ✓")
}

// GetOpenPositions returns all open positions
func (e *ExecutorFast) GetOpenPositions() []*Position {
	return e.positions.GetAllSnapshots()
}

// ClearPositions clears all positions (F9 clear)
func (e *ExecutorFast) ClearPositions() {
	e.positions.Clear()
}

// ═══════════════════════════════════════════════════════════════════════════════
// 2X DETECTION TIMER METHODS
// ═══════════════════════════════════════════════════════════════════════════════

// trackTokenFor2X starts tracking a token for 2X timing comparison
// Called on any 50% entry signal (regardless of buy success)
func (e *ExecutorFast) trackTokenFor2X(mint, tokenName string, initialMC float64) {
	e.trackedTokensMu.Lock()
	defer e.trackedTokensMu.Unlock()

	// Don't re-track if already tracking
	if _, exists := e.trackedTokens[mint]; exists {
		return
	}

	e.trackedTokens[mint] = &TrackedToken{
		Mint:       mint,
		TokenName:  tokenName,
		EntryTime:  time.Now(),
		Subscribed: false,
		InitialMC:  initialMC,
	}

	log.Info().
		Str("token", tokenName).
		Str("mint", safeMintPrefix(mint)).
		Float64("mc", initialMC).
		Msg("📊 Started 2X tracking timer")

	// Fetch actual supply async
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		supply, err := e.rpc.GetTokenSupply(ctx, mint)
		if err != nil {
			log.Debug().Err(err).Str("mint", mint).Msg("Failed to fetch token supply for MC tracking")
			return
		}

		if supply > 0 {
			e.trackedTokensMu.Lock()
			if t, ok := e.trackedTokens[mint]; ok {
				t.TotalSupply = supply
			}
			e.trackedTokensMu.Unlock()
			log.Debug().Str("mint", mint).Float64("supply", supply).Msg("Updated token supply cache")
		}
	}()
}

// subscribeToPoolFor2X looks up pool address and subscribes for price updates
func (e *ExecutorFast) subscribeToPoolFor2X(mint string) {
	// Get tracked token
	tracked := e.getTrackedToken(mint)
	if tracked == nil {
		return
	}

	// Skip if already subscribed or no price feed available
	if tracked.Subscribed || e.priceFeed == nil {
		return
	}

	// Try to get pool address from Jupiter (use a swap quote to get route info)
	// This is a simplified approach - in production you'd use Raydium/Orca API
	poolAddr := e.lookupPoolAddress(mint)
	if poolAddr == "" {
		log.Debug().
			Str("mint", safeMintPrefix(mint)).
			Msg("Could not find pool address for 2X tracking")
		return
	}

	// Update tracked token with pool address
	e.trackedTokensMu.Lock()
	tracked.PoolAddr = poolAddr
	tracked.Subscribed = true
	e.trackedTokensMu.Unlock()

	// Subscribe to pool for price updates
	if err := e.priceFeed.TrackToken(mint, poolAddr); err != nil {
		log.Warn().Err(err).
			Str("mint", safeMintPrefix(mint)).
			Msg("Failed to subscribe to pool for 2X tracking")
		return
	}

	log.Info().
		Str("mint", safeMintPrefix(mint)).
		Str("pool", safeMintPrefix(poolAddr)).
		Msg("📡 Subscribed to pool for 2X detection")
}

// lookupPoolAddress finds the AMM pool address for a token mint
func (e *ExecutorFast) lookupPoolAddress(mint string) string {
	// 1. For positions we already have, use their known pool address
	if pos := e.positions.Get(mint); pos != nil && pos.PoolAddr != "" {
		return pos.PoolAddr
	}

	// 2. Query Jupiter for a quote to find the AMM Pool Address
	// We simulate a buy of 0.01 SOL to find the best route
	quote, err := e.jupiter.GetQuote(context.Background(), jupiter.SOLMint, mint, 10000000)
	if err != nil {
		log.Debug().Err(err).Str("mint", safeMintPrefix(mint)).Msg("failed to lookup pool via Jupiter")
		return ""
	}

	// 3. Extract AMM Pool ID from the route plan
	if len(quote.RoutePlan) > 0 {
		poolAddr := quote.RoutePlan[0].SwapInfo.AmmKey
		if poolAddr != "" {
			log.Debug().
				Str("mint", safeMintPrefix(mint)).
				Str("pool", safeMintPrefix(poolAddr)).
				Msg("found pool address via Jupiter")
			return poolAddr
		}
	}

	return ""
}

// getTrackedToken returns a tracked token by mint
func (e *ExecutorFast) getTrackedToken(mint string) *TrackedToken {
	e.trackedTokensMu.RLock()
	defer e.trackedTokensMu.RUnlock()
	return e.trackedTokens[mint]
}

// setBotDetected2X records when the bot detected 2X via WebSocket
func (e *ExecutorFast) setBotDetected2X(mint string) {
	e.trackedTokensMu.Lock()
	defer e.trackedTokensMu.Unlock()

	if t, ok := e.trackedTokens[mint]; ok && t.Bot2XTime == nil {
		now := time.Now()
		t.Bot2XTime = &now
		elapsed := now.Sub(t.EntryTime)
		log.Info().
			Str("token", t.TokenName).
			Dur("elapsed", elapsed).
			Msg("🚀 BOT detected 2X")
	}
}

// setTGAnnounced2X records when Telegram announced 2X
func (e *ExecutorFast) setTGAnnounced2X(mint string) {
	e.trackedTokensMu.Lock()
	defer e.trackedTokensMu.Unlock()

	if t, ok := e.trackedTokens[mint]; ok && t.TG2XTime == nil {
		now := time.Now()
		t.TG2XTime = &now
		elapsed := now.Sub(t.EntryTime)
		log.Info().
			Str("token", t.TokenName).
			Dur("elapsed", elapsed).
			Msg("📡 TG announced 2X")
	}
}

// GetTrackedTokens returns all tracked tokens for TUI display (thread-safe copies)
func (e *ExecutorFast) GetTrackedTokens() map[string]*TrackedToken {
	e.trackedTokensMu.RLock()
	defer e.trackedTokensMu.RUnlock()

	// Return a deep copy to prevent concurrent access issues
	result := make(map[string]*TrackedToken, len(e.trackedTokens))
	for k, v := range e.trackedTokens {
		// Create a copy of the struct content
		copy := *v
		result[k] = &copy
	}
	return result
}

// cleanupOldTrackedTokens removes tokens older than 24 hours
func (e *ExecutorFast) cleanupOldTrackedTokens() {
	e.trackedTokensMu.Lock()
	defer e.trackedTokensMu.Unlock()

	cutoff := time.Now().Add(-24 * time.Hour)
	cleaned := 0

	for mint, t := range e.trackedTokens {
		if t.EntryTime.Before(cutoff) {
			delete(e.trackedTokens, mint)
			cleaned++
		}
	}

	if cleaned > 0 {
		log.Debug().Int("count", cleaned).Msg("Cleaned old tracked tokens (24h)")
	}
}

// checkTrackedTokens2X checks if any monitored token hit 2X based on MC growth
func (e *ExecutorFast) checkTrackedTokens2X() {
	// Snapshot map keys to avoid holding lock during RPC calls (simulated)
	// Actually we just need price, which we can get from priceFeed?
	// But calculating MC requires Supply. Assume standard 1B supply for now?
	// Or just use Price * 1B if InitialMC was close.
	// Actually, easier: If InitialMC provided, we calculate TargetMC.
	// We need Current MC. Current MC = Price * Supply.
	// Getting Supply for every token is hard.
	// Heuristic: Most pump.fun tokens correspond to a curve where Price implies MC directly.
	// Usually 1 SOL = X Token.
	// Let's assume Price growth % == MC growth %.
	// So we need Initial PRICE. We don't have it (unless we fetched quote).
	// We have Initial MC.
	// If we assume Supply is constant (valid for 99% of timeframes we care about).
	// Then (CurrentPrice / InitialPrice) == (CurrentMC / InitialMC).
	// Problem: We don't have InitialPrice.

	// SOLUTION: Fetch Supply ONCE when tracking starts?
	// Or, just assume Supply = 1,000,000,000 (standard for Pump/Moonshot).
	// 1B tokens is standard.
	// MC = Price * 1,000,000,000.

	e.trackedTokensMu.RLock()
	defer e.trackedTokensMu.RUnlock()

	for mint, t := range e.trackedTokens {
		if t.Bot2XTime != nil || t.InitialMC == 0 || !t.Subscribed {
			continue
		}

		// Get current price (SOL)
		price := e.priceFeed.GetPrice(mint)
		if price == 0 {
			continue
		}

		// Heuristic MC Calculation
		// Assumption 2: SOL Price = Real-time cached price from Jupiter
		estimatedSolPrice := e.getSolPrice()

		// Use actual supply if fetched, else fallback to 1B
		supply := t.TotalSupply
		if supply == 0 {
			supply = 1_000_000_000.0
		}

		currentMC := price * estimatedSolPrice * supply

		// Check for 2X (+ small buffer to be sure, or exact 2.0?)
		// Config uses TakeProfitMultiple (e.g. 2.0)
		targetMC := t.InitialMC * e.cfg.GetTrading().TakeProfitMultiple

		if currentMC >= targetMC {
			// 2X Hit!
			e.setBotDetected2X(mint)

			log.Info().
				Str("token", t.TokenName).
				Float64("initial_mc", t.InitialMC).
				Float64("current_mc", currentMC).
				Float64("sol_price", estimatedSolPrice).
				Msg("🚀 BOT detected 2X (Virtual MC)")
		}
	}
}

// startSolPriceRoutine fetches SOL price from Jupiter every 5 minutes
func (e *ExecutorFast) startSolPriceRoutine() {
	go func() {
		// Initial fetch
		e.updateSolPrice()

		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				e.updateSolPrice()
			case <-e.stopCh:
				return
			}
		}
	}()
}

// updateSolPrice fetches 1 SOL -> USDC quote
func (e *ExecutorFast) updateSolPrice() {
	if e.jupiter == nil {
		return
	}

	const SOLMint = "So11111111111111111111111111111111111111112"
	// USDC Mint: EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v
	const USDCMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	const SOLAmountLamports = 1_000_000_000

	// Quote for 1 SOL
	quote, err := e.jupiter.GetQuote(context.Background(), SOLMint, USDCMint, SOLAmountLamports)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to update SOL price cache")
		return
	}

	// USDC has 6 decimals
	// Quote OutAmount is actually string in Jupiter API v6 response
	amt, err := strconv.ParseFloat(quote.OutAmount, 64)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to parse SOL price quote")
		return
	}

	price := amt / 1_000_000.0

	e.solPriceMu.Lock()
	e.solPrice = price
	e.solPriceMu.Unlock()

	log.Debug().Float64("price_usd", price).Msg("Updated SOL Price Cache")
}

func (e *ExecutorFast) getSolPrice() float64 {
	e.solPriceMu.RLock()
	defer e.solPriceMu.RUnlock()
	return e.solPrice
}
