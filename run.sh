#!/bin/bash

# ========================================
# AFNEX COMMAND CENTER - Complete Setup & Run
# All-in-One: Build, Telegram Listener, Trading Bot
# ========================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
BOLD='\033[1m'
NC='\033[0m'

# Binary name
BOT_BIN="pump-bot"

# ========================================
# BANNER
# ========================================
clear
echo -e "${CYAN}"
echo "╔════════════════════════════════════════════════════════════╗"
echo "║          ⚡ AFNEX COMMAND CENTER ⚡                        ║"
echo "║            Solana High-Speed Trading                       ║"
echo "╚════════════════════════════════════════════════════════════╝"
echo -e "${NC}"

# ========================================
# STEP 0: KILL PREVIOUS INSTANCES
# ========================================
echo -e "${YELLOW}[0/6] CLEANING UP OLD PROCESSES...${NC}"
pkill -f "bin/${BOT_BIN}" 2>/dev/null
pkill -f "listener.py" 2>/dev/null
sleep 1
pkill -9 -f "bin/${BOT_BIN}" 2>/dev/null
pkill -9 -f "listener.py" 2>/dev/null
echo -e "${GREEN}✓ Cleanup complete${NC}"

# ========================================
# STEP 1: Create directories
# ========================================
mkdir -p data bin telegram logs

# ========================================
# STEP 2: Create .env if not exists
# ========================================
if [ ! -f .env ]; then
    echo -e "${YELLOW}[1/6] ENVIRONMENT SETUP${NC}"
    echo "────────────────────────────────────────"
    
    echo -e "${BLUE}Enter your phone number (with country code, e.g. +8801335626283):${NC}"
    read -r TG_PHONE
    
    echo -e "${BLUE}Enter Telegram channel link (e.g. https://t.me/solearlytrending):${NC}"
    read -r TG_CHANNEL
    
    cat > .env << EOF
# Telegram Credentials
TG_API_ID=32075863
TG_API_HASH=45e0e97af3144bd63476134dba4b660a
TG_PHONE=$TG_PHONE
TG_CHANNEL_LINK=$TG_CHANNEL

# Wallet (Base58 encoded private key - leave empty for auto-generated)
WALLET_PRIVATE_KEY=

# API Keys
SHYFT_API_KEY=48KZbYxP-9e9SpqR
JUPITER_API_KEYS=80b2e1ea-151e-4268-9b05-51d96586c63b,3dd14a47-68df-4af8-8df9-ccb427583fdc,f88bf924-fecf-4bcb-bea6-19a183998af6

# Debug mode
DEBUG=0
EOF
    echo -e "${GREEN}✓ Config saved to .env${NC}"
else
    echo -e "${GREEN}✓ Config exists${NC}"
fi

source .env 2>/dev/null

# ========================================
# STEP 3: Check Go and build bot (CGO-FREE)
# ========================================
echo -e "${YELLOW}[2/6] BUILDING BOT (CGO-FREE)${NC}"
echo "────────────────────────────────────────"

export PATH=$PATH:/usr/local/go/bin
if ! command -v go &> /dev/null; then
    echo -e "${RED}Go not found! Install from https://go.dev/dl/${NC}"
    exit 1
fi

# Always rebuild if source changed OR force rebuild
FORCE_REBUILD=0
if [ ! -f bin/${BOT_BIN} ]; then
    FORCE_REBUILD=1
elif [ "$(find cmd -newer bin/${BOT_BIN} 2>/dev/null)" ]; then
    FORCE_REBUILD=1
elif [ "$(find internal -newer bin/${BOT_BIN} 2>/dev/null)" ]; then
    FORCE_REBUILD=1
fi

if [ $FORCE_REBUILD -eq 1 ]; then
    echo "Building with CGO_ENABLED=0 (pure Go)..."
    go mod tidy 2>&1 | head -5
    CGO_ENABLED=0 go build -o bin/${BOT_BIN} ./cmd/bot 2>&1
    if [ $? -ne 0 ]; then
        echo -e "${RED}Build failed!${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ Bot built successfully (CGO-free)${NC}"
else
    echo -e "${GREEN}✓ Bot binary up to date${NC}"
fi

# ========================================
# STEP 4: Setup Python venv + Dependencies
# ========================================
echo -e "${YELLOW}[3/6] PYTHON SETUP${NC}"
echo "────────────────────────────────────────"

cd telegram

# Create venv if it doesn't exist
if [ ! -f venv/bin/activate ]; then
    python3 -m venv venv 2>/dev/null
    if [ $? -ne 0 ]; then
        echo -e "${RED}Python venv failed. Run: sudo apt install python3-venv${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ Created Python venv${NC}"
fi

# ALWAYS ensure dependencies are installed (fixes missing aiohttp issue)
./venv/bin/python -m pip install -q --upgrade pip 2>/dev/null
./venv/bin/python -m pip install -q telethon python-dotenv aiohttp 2>/dev/null
if [ $? -ne 0 ]; then
    echo -e "${YELLOW}⚠ Some Python packages may have failed to install${NC}"
else
    echo -e "${GREEN}✓ Python dependencies installed${NC}"
fi

cd ..
echo -e "${GREEN}✓ Python ready${NC}"

# ========================================
# STEP 5: Telegram login (if needed)
# ========================================
echo -e "${YELLOW}[4/6] TELEGRAM SETUP${NC}"
echo "────────────────────────────────────────"

if [ ! -f telegram/telegram_session.session ]; then
    echo ""
    echo -e "${CYAN}═══════════════════════════════════════${NC}"
    echo -e "${CYAN}  TELEGRAM LOGIN REQUIRED (one time)   ${NC}"
    echo -e "${CYAN}═══════════════════════════════════════${NC}"
    echo ""
    echo -e "Phone: ${GREEN}$TG_PHONE${NC}"
    echo -e "Channel: ${GREEN}$TG_CHANNEL_LINK${NC}"
    echo ""
    echo -e "${YELLOW}Telegram will send you a code.${NC}"
    echo -e "${YELLOW}Enter it when prompted below.${NC}"
    echo ""
    
    cd telegram
    # Use venv python directly (source activate doesn't work properly with all commands)
    ./venv/bin/python listener.py
    LOGIN_STATUS=$?
    cd ..
    
    if [ ! -f telegram/telegram_session.session ]; then
        echo -e "${RED}Telegram login failed. Run ./run.sh again.${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ Telegram logged in!${NC}"
else
    echo -e "${GREEN}✓ Telegram session exists${NC}"
fi

# ========================================
# STEP 6: START EVERYTHING
# ========================================
echo -e "${YELLOW}[5/6] STARTING TELEGRAM LISTENER${NC}"
echo "────────────────────────────────────────"

# Start Telegram listener in background using venv python DIRECTLY
cd telegram
# CRITICAL: Use full path to venv python, NOT 'python3' (nohup doesn't inherit venv)
# Use PYTHONUNBUFFERED=1 and -u for immediate log output
PYTHONUNBUFFERED=1 nohup ./venv/bin/python -u listener.py > ../data/telegram.log 2>&1 &
export TG_PID=$!
cd ..

sleep 2

if kill -0 $TG_PID 2>/dev/null; then
    echo -e "${GREEN}✓ Telegram listener running (PID: $TG_PID)${NC}"
else
    echo -e "${RED}✗ Telegram listener failed to start!${NC}"
    echo -e "${YELLOW}Check logs: cat data/telegram.log${NC}"
fi

# ========================================
# WALLET CHECK & INSTRUCTIONS
# ========================================
echo -e "${YELLOW}[6/6] WALLET CHECK${NC}"
echo "────────────────────────────────────────"

# Extract wallet address from .env or let bot generate one
if [ -z "$WALLET_PRIVATE_KEY" ]; then
    echo -e "${YELLOW}⚠ No wallet key in .env - bot will auto-generate one${NC}"
    echo -e "${CYAN}After bot starts, check the wallet address in the TUI${NC}"
else
    echo -e "${GREEN}✓ Wallet key configured${NC}"
fi

echo ""
echo -e "${MAGENTA}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${MAGENTA}║                   💰 WALLET FUNDING 💰                      ║${NC}"
echo -e "${MAGENTA}╠════════════════════════════════════════════════════════════╣${NC}"
echo -e "${MAGENTA}║  The bot CANNOT trade with 0 SOL balance!                  ║${NC}"
echo -e "${MAGENTA}║                                                            ║${NC}"
echo -e "${MAGENTA}║  After the TUI starts, check the wallet address shown      ║${NC}"
echo -e "${MAGENTA}║  in the SYSTEM panel (top-left).                           ║${NC}"
echo -e "${MAGENTA}║                                                            ║${NC}"
echo -e "${MAGENTA}║  → Send at least 0.1 SOL to enable trading                 ║${NC}"
echo -e "${MAGENTA}║  → Use Phantom, Solflare, or any Solana wallet             ║${NC}"
echo -e "${MAGENTA}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Cleanup on exit
cleanup() {
    echo -e "\n${YELLOW}Shutting down...${NC}"
    kill $TG_PID 2>/dev/null
    pkill -P $$ 2>/dev/null
    echo -e "${GREEN}✓ Goodbye!${NC}"
}
trap cleanup EXIT

echo -e "${GREEN}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║          🚀 LAUNCHING AFNEX COMMAND CENTER 🚀              ║${NC}"
echo -e "${GREEN}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""

# ========================================
# TUI MODE SELECTION
# ========================================
echo -e "${CYAN}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║                  🎨 SELECT TUI MODE 🎨                      ║${NC}"
echo -e "${CYAN}╠════════════════════════════════════════════════════════════╣${NC}"
echo -e "${CYAN}║  1) ${BOLD}Classic${NC}${CYAN}   - Simple text-based interface               ║${NC}"
echo -e "${CYAN}║  2) ${BOLD}Crossterm${NC}${CYAN} - Panel-based with borders                  ║${NC}"
echo -e "${CYAN}║  3) ${BOLD}Matrix${NC}${CYAN}    - Animated matrix rain theme                ║${NC}"
echo -e "${CYAN}║  4) ${BOLD}Neon${NC}${CYAN}      - Neon command center                       ║${NC}"
echo -e "${CYAN}║  5) ${BOLD}FNEX${NC}${CYAN}      - Professional trading terminal (NEW!)     ║${NC}"
echo -e "${CYAN}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${YELLOW}Enter mode [1-5] (default: 5):${NC} "
read -r MODE_INPUT

# Default to 5 (FNEX) if empty or invalid
case "$MODE_INPUT" in
    1) UI_MODE=1; MODE_NAME="Classic" ;;
    2) UI_MODE=2; MODE_NAME="Crossterm" ;;
    3) UI_MODE=3; MODE_NAME="Matrix" ;;
    4) UI_MODE=4; MODE_NAME="Neon" ;;
    5|"") UI_MODE=5; MODE_NAME="FNEX" ;;
    *) UI_MODE=5; MODE_NAME="FNEX" ;;
esac

echo -e "${GREEN}✓ Selected: $MODE_NAME mode${NC}"
echo ""

# Load environment variables for bot
set -a
source .env 2>/dev/null
set +a

# Export UI_MODE for the bot
export UI_MODE

echo -e "${BLUE}Tip: Press ${BOLD}M${NC}${BLUE} in the TUI to cycle between modes!${NC}"
echo ""

# Run the TUI bot
./bin/${BOT_BIN}
