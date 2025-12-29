#!/usr/bin/env python3
"""
Telegram Signal Listener (FIXED)

Listens to a Telegram channel using Telethon (MTProto) and forwards
messages to the Go bot via HTTP POST.

FIXES:
- Extracts URLs from Markdown entities (not just raw_text)
- Extracts URLs from inline buttons
- Handles text links like [PIMP](url)

Requirements:
    pip install telethon python-dotenv aiohttp

Environment Variables:
    TG_API_ID       - Telegram API ID (from https://my.telegram.org)
    TG_API_HASH     - Telegram API hash
    TG_PHONE        - Your phone number (+1234567890)
    TG_CHANNEL_ID   - Target channel ID (e.g., -1001234567890)
    GO_BOT_ENDPOINT - Go bot URL (default: http://localhost:8080/signal)
"""

import os
import sys
import asyncio
import time
import re

# Handle missing aiohttp gracefully
try:
    import aiohttp
except ImportError:
    print("❌ aiohttp not installed. Installing...")
    import subprocess
    subprocess.check_call([sys.executable, "-m", "pip", "install", "aiohttp"])
    import aiohttp

from dotenv import load_dotenv
from telethon import TelegramClient, events
from telethon.tl.types import MessageEntityTextUrl, MessageEntityUrl

# Load environment variables
load_dotenv()

API_ID = os.getenv("TG_API_ID")
API_HASH = os.getenv("TG_API_HASH")
PHONE = os.getenv("TG_PHONE")
CHANNEL_ID = os.getenv("TG_CHANNEL_ID")
GO_BOT_ENDPOINT = os.getenv("GO_BOT_ENDPOINT", "http://localhost:8080/signal")

# Session file location
SESSION_FILE = "telegram_session"


def validate_env():
    """Validate required environment variables."""
    missing = []
    if not API_ID:
        missing.append("TG_API_ID")
    if not API_HASH:
        missing.append("TG_API_HASH")
    if not PHONE:
        missing.append("TG_PHONE")
    
    # Accept either TG_CHANNEL_ID or TG_CHANNEL_LINK
    channel = os.getenv("TG_CHANNEL_ID") or os.getenv("TG_CHANNEL_LINK")
    if not channel:
        missing.append("TG_CHANNEL_ID or TG_CHANNEL_LINK")
    
    if missing:
        print(f"❌ Missing environment variables: {', '.join(missing)}")
        print("Please set them in .env file or environment")
        sys.exit(1)


def extract_urls_from_entities(message) -> list:
    """
    Extract ALL URLs from message entities (Markdown links).
    
    Telegram stores [text](url) links as MessageEntityTextUrl entities,
    NOT in raw_text! This function extracts them.
    """
    urls = []
    
    if not message.entities:
        return urls
    
    text = message.raw_text or ""
    
    for entity in message.entities:
        # Handle [clickable text](url) format
        if isinstance(entity, MessageEntityTextUrl):
            urls.append(entity.url)
        # Handle plain URLs in text
        elif isinstance(entity, MessageEntityUrl):
            start = entity.offset
            end = entity.offset + entity.length
            url = text[start:end]
            urls.append(url)
    
    return urls


def extract_geckoterminal_ca(urls: list) -> str:
    """Extract CA from GeckoTerminal URLs."""
    pattern = re.compile(r'geckoterminal\.com/solana/tokens/([1-9A-HJ-NP-Za-km-z]{32,44})')
    for url in urls:
        match = pattern.search(url)
        if match:
            return match.group(1)
    return ""


async def send_to_bot(session: aiohttp.ClientSession, text: str, msg_id: int) -> bool:
    """Send message to Go bot via HTTP POST (non-blocking)."""
    try:
        payload = {
            "text": text,
            "msg_id": msg_id,
            "timestamp": int(time.time())
        }
        async with session.post(GO_BOT_ENDPOINT, json=payload, timeout=aiohttp.ClientTimeout(total=5)) as resp:
            if resp.status == 200:
                # Only show first 80 chars
                preview = text.replace('\n', ' ')[:80]
                print(f"✅ Sent: {preview}...")
                return True
            else:
                body = await resp.text()
                print(f"⚠️ Bot returned {resp.status}: {body}")
                return False
    except asyncio.TimeoutError:
        print(f"⚠️ Timeout sending to bot")
        return False
    except aiohttp.ClientError as e:
        print(f"❌ Failed to send to bot: {e}")
        return False


async def main():
    """Main async entry point."""
    validate_env()
    
    print(f"📱 Connecting to Telegram...")
    print(f"   API ID: {API_ID}")
    print(f"   Phone: {PHONE}")
    print(f"   Bot Endpoint: {GO_BOT_ENDPOINT}")
    
    client = TelegramClient(SESSION_FILE, API_ID, API_HASH)
    
    await client.start(phone=PHONE)
    print("✅ Connected to Telegram")
    
    # Get channel entity - support both ID and link
    channel_input = os.getenv("TG_CHANNEL_ID") or os.getenv("TG_CHANNEL_LINK")
    if not channel_input:
        print("❌ Neither TG_CHANNEL_ID nor TG_CHANNEL_LINK is set")
        sys.exit(1)
    
    try:
        # Try to parse as int (channel ID)
        if channel_input.lstrip('-').isdigit():
            channel_id = int(channel_input)
            channel = await client.get_entity(channel_id)
        else:
            # It's a link/username - resolve it
            channel = await client.get_entity(channel_input)
            channel_id = channel.id
        print(f"✅ Listening to channel: {getattr(channel, 'title', channel_id)}")
    except ValueError as e:
        print(f"❌ Invalid channel: {e}")
        sys.exit(1)
    except Exception as e:
        print(f"❌ Failed to get channel: {e}")
        sys.exit(1)
    
    # Create aiohttp session for non-blocking HTTP requests
    async with aiohttp.ClientSession() as http_session:
        @client.on(events.NewMessage(chats=channel_id))
        async def handler(event):
            """Handle new messages from channel."""
            text = event.raw_text
            msg_id = event.id
            
            # Skip empty messages
            if not text or not text.strip():
                return
            
            # FIX: Extract URLs from Markdown entities (critical!)
            urls = extract_urls_from_entities(event.message)
            
            # Append extracted URLs to text for Go bot parsing
            if urls:
                text += "\n[EXTRACTED_URLS]:\n" + "\n".join(urls)
                
                # Also extract CA directly and append
                ca = extract_geckoterminal_ca(urls)
                if ca:
                    text += f"\n[CA]: {ca}"
                    print(f"🔗 Found CA: {ca}")
            
            # Extract buttons if any (backup method)
            if event.message.buttons:
                for row in event.message.buttons:
                    for btn in row:
                        if hasattr(btn, 'url') and btn.url:
                            text += f"\n[BUTTON_URL]: {btn.url}"
            
            print(f"\n📨 [{msg_id}]: {text[:100]}...")
            
            # Send to Go bot (non-blocking async)
            await send_to_bot(http_session, text, msg_id)
        
        print("🎧 Listening for messages... (Ctrl+C to stop)")
        print("=" * 50)
        
        # Run until disconnected
        await client.run_until_disconnected()


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        print("\n👋 Goodbye!")
