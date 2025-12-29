
import os
import sys
import asyncio
from dotenv import load_dotenv
from telethon import TelegramClient

# Load environment variables
load_dotenv()

API_ID = os.getenv("TG_API_ID")
API_HASH = os.getenv("TG_API_HASH")
PHONE = os.getenv("TG_PHONE")
SESSION_FILE = "telegram_session"

async def main():
    if len(sys.argv) < 2:
        print("Usage: python3 fetch_msg.py <msg_link_or_channel>")
        sys.exit(1)

    link = sys.argv[1]
    msg_id = None
    channel_name = link

    # Try to parse t.me/channel/123 vs t.me/channel
    if 't.me/' in link:
        parts = link.split('/')
        # Handle trailing slash
        if not parts[-1]: parts.pop()
        
        # Check if last part is ID
        if parts[-1].isdigit():
            msg_id = int(parts[-1])
            channel_name = parts[-2]
        else:
            channel_name = parts[-1] 
            # If full URL, might be index 3 (https://t.me/channel)
            if 'http' in link:
                for i, p in enumerate(parts):
                    if 't.me' in p and i+1 < len(parts):
                        channel_name = parts[i+1]
                        break

    client = TelegramClient(SESSION_FILE, API_ID, API_HASH)
    await client.start(phone=PHONE)
    
    try:
        # Get entity
        entity = await client.get_entity(channel_name)
        
        message = None
        if msg_id:
            # Get specific message
            message = await client.get_messages(entity, ids=msg_id)
        else:
            # Get LATEST message
            messages = await client.get_messages(entity, limit=1)
            if messages:
                message = messages[0]
        
        if message:
            text = message.raw_text
            
            # Extract buttons if any
            if message.buttons:
                for row in message.buttons:
                    for btn in row:
                        if hasattr(btn, 'url') and btn.url:
                            text += f"\n[BUTTON_URL]: {btn.url}"
            
            print(text)
        else:
            print("Message not found or empty")
            sys.exit(1)
            
    except Exception as e:
        print(f"Error fetching message: {e}")
        sys.exit(1)
    finally:
        await client.disconnect()

if __name__ == "__main__":
    asyncio.run(main())
