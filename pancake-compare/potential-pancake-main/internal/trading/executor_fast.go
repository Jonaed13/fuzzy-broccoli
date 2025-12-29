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
	seen2X            map[string]bool // Track unique mints that hit 2X (prevent double count)
	statsMu           sync.RWMutex

	// Retry config
	maxRetries int

	// Simulation Override
	simMode bool

	// WebSocket Real-Time
	wsClient  *ws.Client
	priceFeed *ws.PriceFeed
	walletMon *ws.WalletMonitor
	stopCh    chan struct{}
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
	return &ExecutorFast{
		cfg:           cfg,
		wallet:        wallet,
		rpc:           rpc,
		jupiter:       jupiterClient,
		txBuilder:     txBuilder,
		positions:     positions,
		balance:       balance,
		db:            db,
		metrics:       NewMetrics(),
		recentSignals: make(map[int64]time.Time),
		recentMints:   make(map[string]time.Time),
		seen2X:        make(map[string]bool),
		maxRetries:    2,
		stopCh:        make(chan struct{}), // FIX: Initialize stopCh in constructor
	}
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
			log.Warn().Err(err).Str("mint", pos.Mint[:8]+"...").Msg("failed to resubscribe")
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
		log.Warn().Str("mint", update.Mint[:8]+"...").Msg("token balance dropped to 0 - removing position")
		e.positions.Remove(update.Mint)
		return
	}

	// Real-time price check (if price available from WebSocket)
	if update.PriceSOL > 0 {
		// Calculate current value
		currentValueSOL := update.PriceSOL * float64(update.TokenBalance)

		// Calculate PnL multiple safely
		multiple := pos.UpdateStats(currentValueSOL, update.TokenBalance)

		// INSTANT 2X CHECK (per ms, not per 5 seconds!)
		cfg := e.cfg.GetTrading()
		if cfg.AutoTradingEnabled && multiple >= cfg.TakeProfitMultiple && !pos.IsReached2X() {
			pos.SetReached2X(true)
			log.Info().
				Str("token", pos.TokenName).
				Float64("multiple", multiple).
				Msg("🚀 REAL-TIME 2X DETECTED - TRIGGERING AUTO-SELL")

			// Trigger sell immediately
			go func() {
				signal := &signalPkg.Signal{
					Mint:      update.Mint,
					TokenName: pos.TokenName,
					Type:      signalPkg.SignalExit,
					Value:     multiple,
					Unit:      "X",
				}
				e.executeSellFast(context.Background(), signal, NewTradeTimer())
			}()
		}

		log.Debug().
			Str("mint", update.Mint[:8]+"...").
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
		log.Info().
			Str("token", signal.TokenName).
			Float64("value", signal.Value).
			Str("unit", signal.Unit).
			Msg("📊 SIGNAL FOUND")
	case signalPkg.SignalExit:
		// NOTE: 2X counting is handled in executeSellFast to prevent double counting
		log.Info().
			Str("token", signal.TokenName).
			Float64("value", signal.Value).
			Msg("📊 2X SIGNAL DETECTED")
	}

	// Check if trading enabled (only for execution, not counting)
	if !e.cfg.GetTrading().AutoTradingEnabled {
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
		log.Warn().
			Str("token", signal.TokenName).
			Int("current", e.positions.Count()).
			Msg("❌ MAX POSITIONS REACHED - skipping buy")
		return fmt.Errorf("max open positions reached")
	}

	// Check if we already have this position
	if e.hasMintPosition(signal.Mint) {
		// Update CurrentValue and PnL even if we don't buy
		pos := e.positions.Get(signal.Mint)
		if pos != nil {
			pos.SetStatsFromSignal(signal.Value, signal.Unit)
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
		log.Error().
			Str("token", signal.TokenName).
			Msg("❌ CANNOT BUY: Wallet balance is 0 SOL! Fund your wallet.")
		return fmt.Errorf("wallet balance is 0 - fund your wallet to trade")
	}

	// FAIL LOUDLY if balance is too low for minimum trade
	if balanceLamports < MinTradeLamports {
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
			backoffMs := 100 * (1 << (attempt - 1)) // 100ms, 200ms, 400ms, 800ms...
			log.Warn().Int("attempt", attempt+1).Int("backoffMs", backoffMs).Msg("retrying buy...")
			time.Sleep(time.Duration(backoffMs) * time.Millisecond)
		}

		// Simulation Mode Bypass
		// This is the primary simulation mode bypass. It prevents the bot from
		// making any real trades or sending any transactions.
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
	return lastErr
}

// executeSellFast - FIRE AND FORGET sell execution with retry
func (e *ExecutorFast) executeSellFast(ctx context.Context, signal *signalPkg.Signal, timer *TradeTimer) error {
	// Update position value for TUI display before selling
	if pos := e.positions.Get(signal.Mint); pos != nil {
		pos.SetStatsFromSignal(signal.Value, signal.Unit)

		// FIX: Prevent double counting of 2X hits
		if !pos.IsReached2X() {
			pos.SetReached2X(true)
			e.Increment2XHit()
		}
		e.positions.Add(pos) // Update DB
	}

	// FIX #2 & #6: Get actual token balance instead of max uint64
	tokenAmount, err := e.getTokenBalance(ctx, signal.Mint)
	if err != nil || tokenAmount == 0 {
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
}

func (e *ExecutorFast) Increment2XHit() {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	e.reached2X++
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
}

// GetMetrics returns the metrics tracker
func (e *ExecutorFast) GetMetrics() *Metrics {
	return e.metrics
}

// SellAllPositions triggers a ForceClose for every active position
func (e *ExecutorFast) SellAllPositions(ctx context.Context) {
	positions := e.positions.GetAll()
	log.Warn().Int("count", len(positions)).Msg("🚨 PANIC SELL TRIGGERED: Selling ALL positions")

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
	positions := e.positions.GetAll() // Get live pointers to update them
	if len(positions) == 0 {
		return
	}

	cfg := e.cfg.GetTrading()

	// ⚡ Bolt Optimization: Parallelize position monitoring
	// Use a semaphore to limit concurrency and avoid API rate limits
	const maxConcurrentChecks = 5
	sem := make(chan struct{}, maxConcurrentChecks)
	var wg sync.WaitGroup

	for _, pos := range positions {
		wg.Add(1)
		go func(pos *Position) {
			defer wg.Done()

			// Optimization: Skip RPC check if position was updated recently via WebSocket
			if time.Since(pos.GetLastUpdate()) < 2*time.Second {
				return
			}

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// FIX: Handle stale PENDING positions (failed buys)
			if pos.GetEntryTxSig() == "PENDING" {
				// If pending for more than PendingPositionTTL, mark as failed
				if time.Since(pos.EntryTime) > PendingPositionTTL {
					log.Warn().
						Str("token", pos.TokenName).
						Dur("age", time.Since(pos.EntryTime)).
						Msg("removing stale PENDING position (buy likely failed)")
					e.positions.Remove(pos.Mint)
					return
				}
			}

			// Get current token balance
			balance, err := e.getTokenBalance(ctx, pos.Mint)

			// FIX: Handle 0 balance - mark position as lost/failed
			if err != nil {
				log.Debug().Err(err).Str("mint", pos.Mint[:8]+"...").Msg("failed to get balance")
				return
			}

			if balance == 0 {
				// Position has 0 tokens - either sold externally or buy failed
				if pos.GetEntryTxSig() != "PENDING" && pos.GetEntryTxSig() != "FAILED" {
					// Was a real position that now has 0 tokens
					log.Warn().
						Str("token", pos.TokenName).
						Msg("position has 0 tokens - marking as sold/failed")
					pos.SetStatsFromSignal(0, "X") // safe update
					pos.PnLPercent = -100 // Show as total loss
					pos.SetEntryTxSig("FAILED")
					// Keep it visible for FailedPositionTTL then remove
					if time.Since(pos.EntryTime) > FailedPositionTTL {
						e.positions.Remove(pos.Mint)
					}
				}
				return
			}

			// Get Quote for ALL tokens -> SOL
			quote, err := e.jupiter.GetQuote(ctx, pos.Mint, jupiter.SOLMint, balance)
			if err != nil {
				return
			}

			outAmount, _ := strconv.ParseUint(quote.OutAmount, 10, 64)
			currentValSOL := float64(outAmount) / 1e9

			// Update Position Stats safely
			multiple := pos.UpdateStats(currentValSOL, balance)

			if multiple >= cfg.TakeProfitMultiple { // Use config multiple (e.g. 2.0)
				if !pos.IsReached2X() {
					pos.SetReached2X(true)
					log.Info().Str("token", pos.TokenName).Float64("mult", multiple).Msg("reached target! marked as win")
					e.Increment2XHit()
				}

				// Trigger Auto-Sell
				if cfg.AutoTradingEnabled {
					log.Info().Str("token", pos.TokenName).Msg("triggering take-profit sell")

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
					go e.executeSellFast(ctx, exitSig, timer)
				}
			}

			// Logic: Partial Profit-Taking
			if cfg.PartialProfitPercent > 0 && cfg.PartialProfitMultiple > 1.0 {
				if multiple >= cfg.PartialProfitMultiple && !pos.IsPartialSold() {
					log.Info().Str("token", pos.TokenName).Float64("mult", multiple).Msg("triggering partial profit take")
					e.executePartialSell(ctx, pos, cfg.PartialProfitPercent)
				}
			}

			// Logic: Time-Based Exit
			if cfg.MaxHoldMinutes > 0 {
				if time.Since(pos.EntryTime) > time.Duration(cfg.MaxHoldMinutes)*time.Minute {
					log.Info().Str("token", pos.TokenName).Msg("max hold time reached, selling all")
					// Create a fake signal to trigger full sell
					sig := &signalPkg.Signal{
						Mint:      pos.Mint,
						TokenName: pos.TokenName,
						Type:      signalPkg.SignalExit,
						Value:     currentValSOL,
					}
					e.executeSellFast(ctx, sig, NewTradeTimer())
				}
			}
		}(pos)
	}

	wg.Wait()
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
	pos.SetPartialSold(true)
	log.Info().Str("txSig", txSig).Msg("PARTIAL SELL executed ✓")
}

// GetOpenPositions returns all open positions (safe copies for TUI)
func (e *ExecutorFast) GetOpenPositions() []*Position {
	return e.positions.GetAllSnapshots()
}

// ClearPositions clears all positions (F9 clear)
func (e *ExecutorFast) ClearPositions() {
	e.positions.Clear()
}
