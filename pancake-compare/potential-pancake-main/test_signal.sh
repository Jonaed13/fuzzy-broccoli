#!/bin/bash
# test_signal.sh - Verify a signal from a Telegram Link
# Usage: ./test_signal.sh https://t.me/channel/123

export PATH=$PATH:/usr/local/go/bin

if [ -z "$1" ]; then
    # Try to load from .env
    if [ -f .env ]; then
        source .env
        LINK="$TG_CHANNEL_LINK"
    fi
    
    if [ -z "$LINK" ]; then
        echo "Usage: ./test_signal.sh <telegram_message_link_or_channel>"
        exit 1
    fi
    echo "ℹ️  Using default channel: $LINK"
else
    LINK="$1"
fi

echo "1. Fetching message from Telegram..."
cd telegram
source venv/bin/activate
MSG=$(python3 fetch_msg.py "$LINK")
EXIT_CODE=$?
deactivate
cd ..

if [ $EXIT_CODE -ne 0 ]; then
    echo "❌ Failed to fetch message"
    exit 1
fi

echo "2. Verifying signal logic..."
echo "$MSG" | go run ./cmd/verify-signal
