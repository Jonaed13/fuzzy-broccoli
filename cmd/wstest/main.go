package main

import (
	"encoding/json"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"solana-pump-bot/internal/config"
	ws "solana-pump-bot/internal/websocket"
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"})
	log.Info().Msg("🔌 WebSocket Connection Test")

	// Load config
	cfg, err := config.NewManager("config/config.yaml")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}
	time.Sleep(500 * time.Millisecond)

	wsCfg := cfg.Get().WebSocket
	log.Info().Str("url", wsCfg.ShyftURL[:40]+"...").Msg("connecting to Shyft WebSocket")

	// Create WebSocket client
	client := ws.NewClient(
		wsCfg.ShyftURL,
		time.Duration(wsCfg.ReconnectDelayMs)*time.Millisecond,
		time.Duration(wsCfg.PingIntervalMs)*time.Millisecond,
	)

	client.SetCallbacks(
		func() {
			log.Info().Msg("✅ WebSocket CONNECTED!")
			
			// Test: Subscribe to SOL mint account (just to verify subscription works)
			solMint := "So11111111111111111111111111111111111111112"
			subID, err := client.AccountSubscribe(solMint, func(data json.RawMessage) {
				log.Info().RawJSON("data", data).Msg("📨 Account update received")
			})
			if err != nil {
				log.Error().Err(err).Msg("subscribe failed")
			} else {
				log.Info().Uint64("subID", subID).Msg("subscribed to SOL mint")
			}
		},
		func(err error) {
			log.Warn().Err(err).Msg("❌ WebSocket DISCONNECTED")
		},
	)

	if err := client.Connect(); err != nil {
		log.Fatal().Err(err).Msg("connection failed")
	}

	log.Info().Msg("✅ WebSocket test successful! Press Ctrl+C to exit...")

	// Wait for interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	client.Close()
	log.Info().Msg("WebSocket closed")
}
