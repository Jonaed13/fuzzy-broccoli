#!/bin/bash

# ========================================
# AFNEX Trading Bot - First Time Setup
# ========================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

clear
echo -e "${CYAN}"
echo "╔═══════════════════════════════════════════╗"
echo "║      ⚡ AFNEX BOT - FIRST TIME SETUP      ║"
echo "║            Made by Afnex                  ║"
echo "╚═══════════════════════════════════════════╝"
echo -e "${NC}"

# Check if .env exists
if [ ! -f .env ]; then
    echo -e "${RED}Creating .env file...${NC}"
    cat > .env << 'EOF'
# Telegram Credentials
TG_API_ID=
TG_API_HASH=
TG_PHONE=
TG_CHANNEL_LINK=

# Wallet (leave empty for auto-generated test wallet)
WALLET_PRIVATE_KEY=

# Debug mode
DEBUG=0
EOF
fi

source .env 2>/dev/null

echo -e "${YELLOW}STEP 1: TELEGRAM SETUP${NC}"
echo "────────────────────────────────────────"

# Check if phone is set
if [ -z "$TG_PHONE" ]; then
    echo -e "${BLUE}Enter your phone number (with country code, e.g. +8801335626283):${NC}"
    read -r phone
    sed -i "s/^TG_PHONE=.*/TG_PHONE=$phone/" .env
    echo -e "${GREEN}✓ Phone saved${NC}"
else
    echo -e "${GREEN}✓ Phone already set: $TG_PHONE${NC}"
fi

# Check if channel is set
source .env 2>/dev/null
if [ -z "$TG_CHANNEL_LINK" ]; then
    echo ""
    echo -e "${BLUE}Enter the Telegram channel link (e.g. https://t.me/solearlytrending):${NC}"
    read -r channel
    sed -i "s|^TG_CHANNEL_LINK=.*|TG_CHANNEL_LINK=$channel|" .env
    echo -e "${GREEN}✓ Channel saved${NC}"
else
    echo -e "${GREEN}✓ Channel already set: $TG_CHANNEL_LINK${NC}"
fi

echo ""
echo -e "${YELLOW}STEP 2: WALLET SETUP${NC}"
echo "────────────────────────────────────────"

source .env 2>/dev/null
if [ -z "$WALLET_PRIVATE_KEY" ]; then
    echo -e "${CYAN}Options:${NC}"
    echo "  1. Use auto-generated wallet (for testing)"
    echo "  2. Enter your own private key (for real trading)"
    echo ""
    echo -e "${BLUE}Choose (1 or 2):${NC}"
    read -r wallet_choice
    
    if [ "$wallet_choice" = "2" ]; then
        echo -e "${BLUE}Enter your Solana private key (base58):${NC}"
        read -r privkey
        sed -i "s/^WALLET_PRIVATE_KEY=.*/WALLET_PRIVATE_KEY=$privkey/" .env
        echo -e "${GREEN}✓ Private key saved${NC}"
    else
        echo -e "${GREEN}✓ Using auto-generated wallet (will show address on startup)${NC}"
    fi
else
    echo -e "${GREEN}✓ Private key already set${NC}"
fi

echo ""
echo -e "${YELLOW}STEP 3: TELEGRAM LOGIN${NC}"
echo "────────────────────────────────────────"
echo -e "${CYAN}On first run, Telegram will ask for a verification code.${NC}"
echo -e "${CYAN}The code will be sent to your Telegram app.${NC}"
echo ""

# Install Python deps
echo -e "${YELLOW}Installing Python dependencies...${NC}"
cd telegram
pip install --user -q telethon python-dotenv requests 2>/dev/null || \
    python3 -m pip install --user -q telethon python-dotenv requests 2>/dev/null || \
    echo -e "${YELLOW}⚠ pip install may have failed, continuing...${NC}"
cd ..

# Build Go bot
echo ""
echo -e "${YELLOW}Building AFNEX Bot...${NC}"
export PATH=$PATH:/usr/local/go/bin
mkdir -p data bin
go build -o bin/afnex-bot ./cmd/bot 2>&1 || {
    echo -e "${RED}Build failed!${NC}"
    exit 1
}
echo -e "${GREEN}✓ Bot built successfully${NC}"

echo ""
echo -e "${GREEN}╔═══════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║           SETUP COMPLETE! ✅              ║${NC}"
echo -e "${GREEN}╚═══════════════════════════════════════════╝${NC}"
echo ""
echo -e "${CYAN}Your configuration:${NC}"
source .env 2>/dev/null
echo -e "  Phone: ${TG_PHONE:-NOT SET}"
echo -e "  Channel: ${TG_CHANNEL_LINK:-NOT SET}"
echo -e "  Wallet: ${WALLET_PRIVATE_KEY:+CUSTOM KEY SET}"
[ -z "$WALLET_PRIVATE_KEY" ] && echo -e "  Wallet: AUTO-GENERATED (fund the address shown at startup)"
echo ""

echo -e "${YELLOW}NEXT STEPS:${NC}"
echo ""
echo -e "  1. Run ${CYAN}./run.sh${NC} to start the bot"
echo "  2. On first run, enter the Telegram verification code"
echo "  3. Fund your wallet with SOL to trade"
echo ""
echo -e "${BLUE}Press Enter to continue...${NC}"
read -r
