package trading

import (
	"testing"
	"time"

	"solana-pump-bot/internal/config"
)

func TestEvaluateExit(t *testing.T) {
	now := time.Now()
	entryTime := now.Add(-10 * time.Minute)

	tests := []struct {
		name          string
		pos           *Position
		currentValSOL float64
		cfg           config.TradingConfig
		now           time.Time
		wantAction    ExitAction
		wantType      ExitType
		wantReason    string
	}{
		{
			name: "Take Profit Hit",
			pos: &Position{
				Size:      1.0,
				EntryTime: entryTime,
			},
			currentValSOL: 2.0, // 2X
			cfg: config.TradingConfig{
				TakeProfitMultiple: 2.0,
			},
			now:        now,
			wantAction: ActionSellAll,
			wantType:   ExitTypeTakeProfit,
			wantReason: "take profit hit: 2.00X >= 2.00X",
		},
		{
			name: "Stop Loss Hit",
			pos: &Position{
				Size:      1.0,
				EntryTime: entryTime,
			},
			currentValSOL: 0.4, // -60%
			cfg: config.TradingConfig{
				StopLossPercent: -50.0,
			},
			now:        now,
			wantAction: ActionSellAll,
			wantType:   ExitTypeStopLoss,
			wantReason: "stop loss hit: -60.00% <= -50.00%",
		},
		{
			name: "Stop Loss Not Hit",
			pos: &Position{
				Size:      1.0,
				EntryTime: entryTime,
			},
			currentValSOL: 0.6, // -40%
			cfg: config.TradingConfig{
				StopLossPercent: -50.0,
			},
			now:        now,
			wantAction: ActionNone,
		},
		{
			name: "Partial Profit Hit",
			pos: &Position{
				Size:        1.0,
				EntryTime:   entryTime,
				PartialSold: false,
			},
			currentValSOL: 1.5, // 1.5X
			cfg: config.TradingConfig{
				PartialProfitMultiple: 1.5,
				PartialProfitPercent:  50.0,
			},
			now:        now,
			wantAction: ActionSellPartial,
			wantType:   ExitTypePartial,
			wantReason: "partial profit hit: 1.50X >= 1.50X",
		},
		{
			name: "Partial Profit Already Sold",
			pos: &Position{
				Size:        1.0,
				EntryTime:   entryTime,
				PartialSold: true,
			},
			currentValSOL: 1.6,
			cfg: config.TradingConfig{
				PartialProfitMultiple: 1.5,
				PartialProfitPercent:  50.0,
			},
			now:        now,
			wantAction: ActionNone,
		},
		{
			name: "Max Hold Time Hit",
			pos: &Position{
				Size:      1.0,
				EntryTime: now.Add(-31 * time.Minute),
			},
			currentValSOL: 1.0,
			cfg: config.TradingConfig{
				MaxHoldMinutes: 30,
			},
			now:        now,
			wantAction: ActionSellAll,
			wantType:   ExitTypeTime,
			wantReason: "max hold time reached: 31m0s > 30m",
		},
		{
			name: "Nothing Hit",
			pos: &Position{
				Size:      1.0,
				EntryTime: entryTime,
			},
			currentValSOL: 1.1,
			cfg: config.TradingConfig{
				TakeProfitMultiple: 2.0,
				StopLossPercent:    -50.0,
				MaxHoldMinutes:     60,
			},
			now:        now,
			wantAction: ActionNone,
		},
		{
			name: "Already Selling",
			pos: &Position{
				Size:    1.0,
				Selling: true,
			},
			currentValSOL: 2.0,
			cfg: config.TradingConfig{
				TakeProfitMultiple: 1.5,
			},
			now:        now,
			wantAction: ActionNone,
			wantReason: "already selling",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateExit(tt.pos, tt.currentValSOL, tt.cfg, tt.now)
			if got.Action != tt.wantAction {
				t.Errorf("Action = %v, want %v", got.Action, tt.wantAction)
			}
			if tt.wantType != "" && got.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", got.Type, tt.wantType)
			}
			if tt.wantReason != "" && got.Reason != tt.wantReason {
				t.Errorf("Reason = %v, want %v", got.Reason, tt.wantReason)
			}
		})
	}
}
