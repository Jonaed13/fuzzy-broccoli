# 🚀 Solana Pump Bot

High-speed CLI trading bot that listens to Telegram signals and executes trades via Jupiter v6 in ~1 second.

## Features

- **Telegram Integration** - Listens to channels via Telethon (MTProto)
- **Signal Parsing** - Extracts token names & values from "X is up Y%" messages
- **Auto-Trading** - Buy at 50%+, sell at 2X+ (configurable)
- **Speed Optimized** - Blockhash cache, skipPreflight, idempotent ATA
- **Real-time TUI** - Dashboard, config modal, hotkeys
- **SQLite Storage** - Positions, trades, signals history

## Quick Start

### 1. Setup Environment

```bash
cp .env.example .env
# Edit .env with your credentials:
# - TG_API_ID, TG_API_HASH (from https://my.telegram.org)
# - TG_PHONE, TG_CHANNEL_ID
# - WALLET_PRIVATE_KEY (Base58 encoded)
# - SHYFT_API_KEY (optional, for paid RPC)
```

### 2. Install Python Dependencies

```bash
cd telegram
pip install -r requirements.txt
```

### 3. Build & Run

```bash
# Build
go build -o bin/pump-bot ./cmd/bot

# Run bot (with TUI)
./bin/pump-bot

# Or headless mode
HEADLESS=1 ./bin/pump-bot
```

### 4. Start Telegram Listener

```bash
cd telegram
python listener.py
```

## TUI Hotkeys

| Key | Action |
|-----|--------|
| `C` | Open config modal |
| `P` | Pause/resume trading |
| `S` | Force sell position |
| `L` | View logs |
| `T` | View trades history |
| `D` | Back to dashboard |
| `Q` | Quit |

## Configuration

Edit `config/config.yaml` or use the TUI config modal:

```yaml
trading:
  min_entry_percent: 50.0      # Buy when "is up 50%"
  take_profit_multiple: 2.0    # Sell when "is up 2.0X"
  max_alloc_percent: 20.0      # 20% of wallet per trade
  max_open_positions: 5        # Max concurrent trades
  auto_trading_enabled: true   # Master switch

fees:
  static_priority_fee_sol: 0.00375  # Priority fee per TX
```

## Token Cache

Add custom tokens to `config/tokens_cache.json`:

```json
{
  "BONK": "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263",
  "WIF": "EKpQGSJtjMFqKZ9KQanSqYXRcF8fBopzLHYxdM65zcjm"
}
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│ Telegram Channel → Telethon (Python) → Go Bot (HTTP :8080) │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│ Signal Parser → Token Resolver → Signal Classifier          │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│ Trading Engine → Jupiter Quote → TX Builder → Shyft RPC    │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│ Position Tracker → SQLite → TUI Dashboard                   │
└─────────────────────────────────────────────────────────────┘
```

## Speed Optimizations

| Optimization | Savings |
|--------------|---------|
| Blockhash cache (300ms) | ~150ms |
| Static priority fee | ~100ms |
| Idempotent ATA | ~200ms |
| Skip preflight | ~500ms |
| **Total** | **~950ms** |

## License

MIT
