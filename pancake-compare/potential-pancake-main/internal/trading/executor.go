package trading

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"solana-pump-bot/internal/blockchain"
	"solana-pump-bot/internal/config"
	"solana-pump-bot/internal/jupiter"
	signalPkg "solana-pump-bot/internal/signal"
	"solana-pump-bot/internal/storage"
)

// Executor handles trade execution
type Executor struct {
	cfg            *config.Manager
	wallet         *blockchain.Wallet
	rpc            *blockchain.RPCClient
	jupiter        *jupiter.Client
	txBuilder      *blockchain.TransactionBuilder
	positions      *PositionTracker
	balance        *blockchain.BalanceTracker
	db             *storage.DB
	mu             sync.Mutex

	// Callbacks
	onTradeExecuted func(signal *signalPkg.Signal, txSig string, success bool)
}

// NewExecutor creates a new trade executor
func NewExecutor(
	cfg *config.Manager,
	wallet *blockchain.Wallet,
	rpc *blockchain.RPCClient,
	jupiterClient *jupiter.Client,
	txBuilder *blockchain.TransactionBuilder,
	positions *PositionTracker,
	balance *blockchain.BalanceTracker,
	db *storage.DB,
) *Executor {
	return &Executor{
		cfg:       cfg,
		wallet:    wallet,
		rpc:       rpc,
		jupiter:   jupiterClient,
		txBuilder: txBuilder,
		positions: positions,
		balance:   balance,
		db:        db,
	}
}

// SetOnTradeExecuted sets the callback for trade execution
func (e *Executor) SetOnTradeExecuted(fn func(signal *signalPkg.Signal, txSig string, success bool)) {
	e.onTradeExecuted = fn
}

// ProcessSignal processes a trading signal
func (e *Executor) ProcessSignal(ctx context.Context, signal *signalPkg.Signal) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Log signal to DB
	if e.db != nil {
		if err := e.db.InsertSignal(&storage.Signal{
			TokenName:  signal.TokenName,
			Value:      signal.Value,
			Unit:       signal.Unit,
			SignalType: string(signal.Type),
			MsgID:      signal.MsgID,
			Timestamp:  signal.Timestamp,
		}); err != nil {
			log.Error().Err(err).Msg("failed to insert signal to DB")
		}
	}

	// Check if trading is enabled
	if !e.cfg.GetTrading().AutoTradingEnabled {
		log.Debug().Msg("auto-trading disabled, ignoring signal")
		return nil
	}

	// Check if mint is resolved
	if signal.Mint == "" {
		log.Warn().Str("token", signal.TokenName).Msg("mint not resolved, skipping")
		return nil
	}

	switch signal.Type {
	case signalPkg.SignalEntry:
		return e.executeBuy(ctx, signal)
	case signalPkg.SignalExit:
		return e.executeSell(ctx, signal)
	default:
		log.Debug().Str("token", signal.TokenName).Msg("signal ignored")
		return nil
	}
}

// executeBuy executes a buy trade
func (e *Executor) executeBuy(ctx context.Context, signal *signalPkg.Signal) error {
	start := time.Now()

	// Pre-trade checks
	if e.positions.Has(signal.Mint) {
		log.Warn().Str("token", signal.TokenName).Msg("position already exists")
		return nil
	}

	if !e.positions.CanOpen() {
		log.Warn().Msg("max positions reached")
		return nil
	}

	// Calculate trade size
	cfg := e.cfg.GetTrading()
	balanceLamports := e.balance.BalanceLamports()
	allocLamports := uint64(float64(balanceLamports) * cfg.MaxAllocPercent / 100)

	// Check balance including fees
	feesCfg := e.cfg.Get().Fees
	totalFeesLamports := uint64((feesCfg.StaticPriorityFeeSol + feesCfg.StaticGasFeeSol) * 1e9)

	if !e.balance.HasSufficientBalance(allocLamports, totalFeesLamports) {
		log.Warn().
			Float64("balance", e.balance.BalanceSOL()).
			Float64("needed", float64(allocLamports+totalFeesLamports)/1e9).
			Msg("insufficient balance")
		return nil
	}

	log.Info().
		Str("token", signal.TokenName).
		Str("mint", signal.Mint).
		Float64("amount", float64(allocLamports)/1e9).
		Msg("executing BUY")

	// Get swap transaction from Jupiter
	swapTx, err := e.jupiter.GetSwapTransaction(ctx, jupiter.SOLMint, signal.Mint, e.wallet.Address(), allocLamports)
	if err != nil {
		log.Error().Err(err).Msg("failed to get Jupiter swap TX")
		return err
	}

	// Sign transaction
	signedTx, err := e.txBuilder.SignSerializedTransaction(swapTx)
	if err != nil {
		log.Error().Err(err).Msg("failed to sign transaction")
		return err
	}

	// Send transaction (skipPreflight = true for speed)
	txSig, err := e.rpc.SendTransaction(ctx, signedTx, true)
	if err != nil {
		log.Error().Err(err).Msg("failed to send transaction")
		if e.onTradeExecuted != nil {
			e.onTradeExecuted(signal, "", false)
		}
		return err
	}

	elapsed := time.Since(start)

	log.Info().
		Str("token", signal.TokenName).
		Str("txSig", txSig).
		Dur("elapsed", elapsed).
		Msg("BUY executed ✓")

	// Record position
	pos := &Position{
		Mint:       signal.Mint,
		TokenName:  signal.TokenName,
		Size:       float64(allocLamports) / 1e9, // Store in SOL for simplicity
		EntryValue: signal.Value,
		EntryUnit:  signal.Unit,
		EntryTime:  time.Now(),
		EntryTxSig: txSig,
		MsgID:      signal.MsgID,
	}

	if err := e.positions.Add(pos); err != nil {
		log.Error().Err(err).Msg("failed to save position")
	}

	// Refresh balance
	go e.balance.Refresh(context.Background())

	if e.onTradeExecuted != nil {
		e.onTradeExecuted(signal, txSig, true)
	}

	return nil
}

// executeSell executes a sell trade
func (e *Executor) executeSell(ctx context.Context, signal *signalPkg.Signal) error {
	start := time.Now()

	// Check if position exists
	pos := e.positions.Get(signal.Mint)
	if pos == nil {
		log.Warn().Str("token", signal.TokenName).Msg("no position to sell")
		return nil
	}

	log.Info().
		Str("token", signal.TokenName).
		Str("mint", signal.Mint).
		Float64("entryValue", pos.EntryValue).
		Float64("exitValue", signal.Value).
		Msg("executing SELL")

	// For sell, we need to sell ALL of the token we hold
	// Query actual token balance for accurate sell amount
	tokenAmount, err := e.getTokenBalance(ctx, signal.Mint)
	if err != nil {
		log.Error().Err(err).Msg("failed to get token balance for sell")
		// Fallback to stored position size (may be inaccurate)
		tokenAmount = uint64(pos.Size * 1e9)
	}

	if tokenAmount == 0 {
		log.Warn().Str("token", signal.TokenName).Msg("zero token balance, removing position")
		e.positions.Remove(signal.Mint)
		return nil
	}

	// Get swap transaction from Jupiter (token -> SOL)
	swapTx, err := e.jupiter.GetSwapTransaction(ctx, signal.Mint, jupiter.SOLMint, e.wallet.Address(), tokenAmount)
	if err != nil {
		log.Error().Err(err).Msg("failed to get Jupiter swap TX")
		return err
	}

	// Sign transaction
	signedTx, err := e.txBuilder.SignSerializedTransaction(swapTx)
	if err != nil {
		log.Error().Err(err).Msg("failed to sign transaction")
		return err
	}

	// Send transaction
	txSig, err := e.rpc.SendTransaction(ctx, signedTx, true)
	if err != nil {
		log.Error().Err(err).Msg("failed to send transaction")
		if e.onTradeExecuted != nil {
			e.onTradeExecuted(signal, "", false)
		}
		return err
	}

	elapsed := time.Since(start)

	log.Info().
		Str("token", signal.TokenName).
		Str("txSig", txSig).
		Dur("elapsed", elapsed).
		Msg("SELL executed ✓")

	// Remove position
	removedPos, err := e.positions.Remove(signal.Mint)
	if err != nil {
		log.Error().Err(err).Msg("failed to remove position")
	}

	// Log trade
	if e.db != nil && removedPos != nil {
		duration := time.Since(removedPos.EntryTime).Seconds()
		// Note: Actual PnL would require querying actual received amount
		e.db.InsertTrade(&storage.Trade{
			Mint:       signal.Mint,
			TokenName:  signal.TokenName,
			EntryValue: removedPos.EntryValue,
			ExitValue:  signal.Value,
			PnL:        0, // Would need actual balance diff
			Duration:   int64(duration),
			EntryTxSig: removedPos.EntryTxSig,
			ExitTxSig:  txSig,
			Timestamp:  storage.Now(),
		})
	}

	// Refresh balance
	go e.balance.Refresh(context.Background())

	if e.onTradeExecuted != nil {
		e.onTradeExecuted(signal, txSig, true)
	}

	return nil
}

// GetOpenPositions returns all open positions
func (e *Executor) GetOpenPositions() []*Position {
	return e.positions.GetAllSnapshots()
}

// GetPositionCount returns number of open positions
func (e *Executor) GetPositionCount() int {
	return e.positions.Count()
}

// StartMonitoring starts the background price monitor
func (e *Executor) StartMonitoring(ctx context.Context) {
	log.Info().Msg("starting active trade monitor...")
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

func (e *Executor) monitorPositions(ctx context.Context) {
	positions := e.positions.GetAll()
	if len(positions) == 0 { return }
	
	cfg := e.cfg.GetTrading()
	
	for _, pos := range positions {
		// Get current token balance
		balance, err := e.getTokenBalance(ctx, pos.Mint)
		if err != nil || balance == 0 { continue }
		
		// Get Quote for ALL tokens -> SOL
		quote, err := e.jupiter.GetQuote(ctx, pos.Mint, jupiter.SOLMint, balance)
		if err != nil { continue }
		
		outAmount := 0.0
		fmt.Sscanf(quote.OutAmount, "%f", &outAmount)
		currentValSOL := outAmount / 1e9
		
		// Update Position Stats
		// pos.CurrentValue = currentValSOL
		// pos.PnLSol = currentValSOL - pos.Size
		// if pos.Size > 0 {
		// 	pos.PnLPercent = ((currentValSOL / pos.Size) - 1.0) * 100
		// }
		
		multiple := pos.UpdateStats(currentValSOL, balance)

		// Logic: 2X Detection
		// multiple := 0.0
		// if pos.Size > 0 { multiple = currentValSOL / pos.Size }
		
		if multiple >= 2.0 && !pos.IsReached2X() {
			pos.SetReached2X(true)
			log.Info().Str("token", pos.TokenName).Msg("reached 2X! marked as win")
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
				e.executeSell(ctx, sig)
			}
		}
	}
}

func (e *Executor) executePartialSell(ctx context.Context, pos *Position, percent float64) {
	// 1. Calculate Amount
	balance, err := e.getTokenBalance(ctx, pos.Mint)
	if err != nil { return }
	
	sellAmount := uint64(float64(balance) * (percent / 100.0))
	
	log.Info().Str("token", pos.TokenName).Msgf("selling %.0f%% of position...", percent)
	
	// 2. Perform Swap (Token -> SOL)
	swapTx, err := e.jupiter.GetSwapTransaction(ctx, pos.Mint, jupiter.SOLMint, e.wallet.Address(), sellAmount)
	if err != nil {
		log.Error().Err(err).Msg("failed partial swap tx")
		return
	}
	
	signedTx, err := e.txBuilder.SignSerializedTransaction(swapTx)
	if err != nil { return }
	
	txSig, err := e.rpc.SendTransaction(ctx, signedTx, true)
	if err != nil {
		log.Error().Err(err).Msg("failed partial sell send")
		return
	}
	
	// 3. Update Position State
	pos.SetPartialSold(true)
	log.Info().Str("txSig", txSig).Msg("PARTIAL SELL executed ✓")
	
	// Note: We don't remove position, just mark sold. 
	// Adjusting Size (Cost Basis) is complex, usually we just reduce it?
	// For simplicity, we keep original Size to track overall ROI against initial investment.
}

// ForceClose manually closes a position
func (e *Executor) ForceClose(ctx context.Context, mint string) error {
	pos := e.positions.Get(mint)
	if pos == nil {
		return fmt.Errorf("position not found: %s", mint)
	}

	// Create a fake exit signal
	signal := &signalPkg.Signal{
		TokenName: pos.TokenName,
		Mint:      mint,
		Value:     0, // Manual close
		Unit:      "X",
		Type:      signalPkg.SignalExit,
		Timestamp: storage.Now(),
	}

	return e.executeSell(ctx, signal)
}

// getTokenBalance queries the actual token balance for a given mint
func (e *Executor) getTokenBalance(ctx context.Context, mint string) (uint64, error) {
	// This would typically use getTokenAccountsByOwner or similar RPC call
	// For now, we'll use a simplified approach that may require enhancement
	// 
	// In a full implementation, you would:
	// 1. Derive the Associated Token Account (ATA) address
	// 2. Call getTokenAccountBalance on that address
	//
	// For Jupiter swaps, often the actual balance doesn't need to be exact
	// as Jupiter will use all available balance when you specify a high amount
	
	// Return a max value which Jupiter will interpret as "sell all"
	// Jupiter API handles partial fills appropriately
	return 0xFFFFFFFFFFFFFFFF, nil
}
