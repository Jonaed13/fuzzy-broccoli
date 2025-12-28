package trading

import (
	"fmt"
	"time"

	"solana-pump-bot/internal/config"
)

// ExitAction represents the decision made by the exit evaluator
type ExitAction string

const (
	ActionNone        ExitAction = "NONE"
	ActionSellAll     ExitAction = "SELL_ALL"
	ActionSellPartial ExitAction = "SELL_PARTIAL"
)

// ExitType represents the type of exit reason
type ExitType string

const (
	ExitTypeNone         ExitType = "NONE"
	ExitTypeStopLoss     ExitType = "STOP_LOSS"
	ExitTypeTakeProfit   ExitType = "TAKE_PROFIT"
	ExitTypePartial      ExitType = "PARTIAL_PROFIT"
	ExitTypeTime         ExitType = "TIME_LIMIT"
	ExitTypeManual       ExitType = "MANUAL"
)

// ExitDecision holds the result of the exit evaluation
type ExitDecision struct {
	Action ExitAction
	Type   ExitType
	Reason string
}

// evaluateExit determines if a position should be sold based on current state and config
// pure function for easy testing
func evaluateExit(pos *Position, currentValSOL float64, cfg config.TradingConfig, now time.Time) ExitDecision {
	// If already selling, do nothing (prevent duplicates)
	// Note: The caller should handle the 'Selling' flag check to prevent re-entry,
	// but we can also return ActionNone if we passed the flag in.
	// For purity, we assume 'pos' reflects current state.

	if pos.Selling {
		return ExitDecision{Action: ActionNone, Reason: "already selling"}
	}

	// Calculate stats
	// Note: We don't update 'pos' here to keep it pure. We calculate locally.
	// In the real executor, 'pos' is updated before calling this or we use these values.
	// Let's use the passed currentValSOL.

	multiple := 0.0
	pnlPercent := 0.0

	if pos.Size > 0 {
		multiple = currentValSOL / pos.Size
		pnlPercent = (multiple - 1.0) * 100
	} else {
		// Invalid position size?
		return ExitDecision{Action: ActionNone, Reason: "invalid position size"}
	}

	// 1. Stop Loss Check
	// e.g. StopLossPercent = -50.0. If pnlPercent (-60) <= -50, SELL.
	if cfg.StopLossPercent < 0 {
		if pnlPercent <= cfg.StopLossPercent {
			return ExitDecision{
				Action: ActionSellAll,
				Type:   ExitTypeStopLoss,
				Reason: fmt.Sprintf("stop loss hit: %.2f%% <= %.2f%%", pnlPercent, cfg.StopLossPercent),
			}
		}
	}

	// 2. Take Profit (2X) Check
	if cfg.TakeProfitMultiple > 1.0 && multiple >= cfg.TakeProfitMultiple {
		return ExitDecision{
			Action: ActionSellAll,
			Type:   ExitTypeTakeProfit,
			Reason: fmt.Sprintf("take profit hit: %.2fX >= %.2fX", multiple, cfg.TakeProfitMultiple),
		}
	}

	// 3. Partial Profit Check
	if cfg.PartialProfitPercent > 0 && cfg.PartialProfitMultiple > 1.0 {
		if multiple >= cfg.PartialProfitMultiple && !pos.PartialSold {
			return ExitDecision{
				Action: ActionSellPartial,
				Type:   ExitTypePartial,
				Reason: fmt.Sprintf("partial profit hit: %.2fX >= %.2fX", multiple, cfg.PartialProfitMultiple),
			}
		}
	}

	// 4. Time-Based Exit
	if cfg.MaxHoldMinutes > 0 {
		if now.Sub(pos.EntryTime) > time.Duration(cfg.MaxHoldMinutes)*time.Minute {
			return ExitDecision{
				Action: ActionSellAll,
				Type:   ExitTypeTime,
				Reason: fmt.Sprintf("max hold time reached: %v > %dm", now.Sub(pos.EntryTime), cfg.MaxHoldMinutes),
			}
		}
	}

	return ExitDecision{Action: ActionNone, Type: ExitTypeNone}
}
