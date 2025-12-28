package signal

import (
	"testing"
)

func TestParser_Parse(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name      string
		text      string
		msgID     int64
		wantToken string
		wantValue float64
		wantUnit  string
		wantMint  string
		wantMC    float64
	}{
		{
			name:      "Standard Percentage Signal",
			text:      "📈 TOKEN is up 50% 📈",
			msgID:     123,
			wantToken: "TOKEN",
			wantValue: 50.0,
			wantUnit:  "%",
		},
		{
			name:      "Standard Multiplier Signal",
			text:      "📈 TOKEN is up 2.5X 📈",
			msgID:     124,
			wantToken: "TOKEN",
			wantValue: 2.5,
			wantUnit:  "X",
		},
		{
			name:      "Signal with GeckoTerminal URL",
			text:      "📈 BONK is up 100% 📈 https://www.geckoterminal.com/solana/tokens/DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263",
			msgID:     125,
			wantToken: "BONK",
			wantValue: 100.0,
			wantUnit:  "%",
			wantMint:  "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263",
		},
		{
			name:      "Signal with Pump.fun URL",
			text:      "📈 PEPE is up 50% 📈 https://pump.fun/DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263",
			msgID:     126,
			wantToken: "PEPE",
			wantValue: 50.0,
			wantUnit:  "%",
			wantMint:  "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263",
		},
		{
			name:      "Signal with Market Cap K",
			text:      "📈 DOGE is up 10% 📈 MC: $35.4K",
			msgID:     127,
			wantToken: "DOGE",
			wantValue: 10.0,
			wantUnit:  "%",
			wantMC:    35400,
		},
		{
			name:      "Signal with Market Cap M",
			text:      "📈 SHIB is up 20% 📈 💰 MC: $1.2M",
			msgID:     128,
			wantToken: "SHIB",
			wantValue: 20.0,
			wantUnit:  "%",
			wantMC:    1200000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal, err := parser.Parse(tt.text, tt.msgID)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if signal == nil {
				t.Fatal("Parse() returned nil signal")
			}

			if signal.TokenName != tt.wantToken {
				t.Errorf("TokenName = %v, want %v", signal.TokenName, tt.wantToken)
			}
			if signal.Value != tt.wantValue {
				t.Errorf("Value = %v, want %v", signal.Value, tt.wantValue)
			}
			if signal.Unit != tt.wantUnit {
				t.Errorf("Unit = %v, want %v", signal.Unit, tt.wantUnit)
			}
			if tt.wantMint != "" && signal.Mint != tt.wantMint {
				t.Errorf("Mint = %v, want %v", signal.Mint, tt.wantMint)
			}
			if tt.wantMC != 0 && signal.InitialMC != tt.wantMC {
				t.Errorf("InitialMC = %v, want %v", signal.InitialMC, tt.wantMC)
			}
		})
	}
}

func TestParser_extractCA(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "GeckoTerminal URL",
			text: "Check this out: https://www.geckoterminal.com/solana/tokens/DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263",
			want: "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263",
		},
		{
			name: "Pump.fun URL",
			text: "Pump it: https://pump.fun/DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263",
			want: "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263",
		},
		{
			name: "Raw Base58 Address",
			text: "Contract: DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263",
			want: "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263",
		},
		{
			name: "GeckoTerminal Priority",
			// RIGHT is 32 chars long, using valid Base58 chars (no 0, O, I, l)
			text: "https://pump.fun/DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263 https://www.geckoterminal.com/solana/tokens/12345678912345678912345678912345",
			want: "12345678912345678912345678912345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.extractCA(tt.text)
			if got != tt.want {
				t.Errorf("extractCA() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParser_Classify(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name               string
		signal             *Signal
		minEntryPercent    float64
		takeProfitMultiple float64
		wantType           SignalType
	}{
		{
			name: "Entry Signal",
			signal: &Signal{
				Value: 60.0,
				Unit:  "%",
				Type:  SignalIgnore,
			},
			minEntryPercent:    50.0,
			takeProfitMultiple: 2.0,
			wantType:           SignalEntry,
		},
		{
			name: "Ignore Entry Signal (Too Low)",
			signal: &Signal{
				Value: 40.0,
				Unit:  "%",
				Type:  SignalIgnore,
			},
			minEntryPercent:    50.0,
			takeProfitMultiple: 2.0,
			wantType:           SignalIgnore,
		},
		{
			name: "Exit Signal",
			signal: &Signal{
				Value: 2.5,
				Unit:  "X",
				Type:  SignalIgnore,
			},
			minEntryPercent:    50.0,
			takeProfitMultiple: 2.0,
			wantType:           SignalExit,
		},
		{
			name: "Ignore Exit Signal (Too Low)",
			signal: &Signal{
				Value: 1.5,
				Unit:  "X",
				Type:  SignalIgnore,
			},
			minEntryPercent:    50.0,
			takeProfitMultiple: 2.0,
			wantType:           SignalIgnore,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser.Classify(tt.signal, tt.minEntryPercent, tt.takeProfitMultiple)
			if tt.signal.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", tt.signal.Type, tt.wantType)
			}
		})
	}
}
