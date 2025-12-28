package websocket

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"sync"

	"github.com/rs/zerolog/log"
)

// Raydium AMM Program ID
const RaydiumAMMProgramID = "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8"

// PriceUpdate represents a real-time price change
type PriceUpdate struct {
	Mint         string
	PriceSOL     float64 // Price in SOL per token
	TokenBalance uint64  // Your token balance
	PoolReserves PoolReserves
	Slot         uint64
}

// PoolReserves holds AMM pool state
type PoolReserves struct {
	BaseReserve   uint64 // Token amount
	QuoteReserve  uint64 // SOL amount (in lamports)
	BaseDecimals  int
	QuoteDecimals int
}

// PriceHandler is called when price updates are received
type PriceHandler func(update PriceUpdate)

// PriceFeed manages real-time price subscriptions for tokens
type PriceFeed struct {
	client *Client

	// Tracked tokens: mint -> pool subscription ID
	poolSubs map[string]uint64
	// Token account subs: mint -> token account subscription ID
	tokenSubs map[string]uint64
	subsMu    sync.RWMutex

	// Pool addresses cache: mint -> pool address
	poolAddrs map[string]string

	// Price callbacks
	handlers   []PriceHandler
	handlersMu sync.RWMutex

	// Wallet address for token account lookups
	walletAddr string

	// Last known prices
	prices   map[string]float64
	pricesMu sync.RWMutex
}

// NewPriceFeed creates a new price feed manager
func NewPriceFeed(client *Client, walletAddr string) *PriceFeed {
	return &PriceFeed{
		client:     client,
		poolSubs:   make(map[string]uint64),
		tokenSubs:  make(map[string]uint64),
		poolAddrs:  make(map[string]string),
		prices:     make(map[string]float64),
		walletAddr: walletAddr,
	}
}

// OnPriceUpdate registers a price update handler
func (p *PriceFeed) OnPriceUpdate(handler PriceHandler) {
	p.handlersMu.Lock()
	p.handlers = append(p.handlers, handler)
	p.handlersMu.Unlock()
}

// TrackToken starts tracking a token's price via AMM pool subscription
func (p *PriceFeed) TrackToken(mint string, poolAddr string) error {
	p.subsMu.Lock()
	defer p.subsMu.Unlock()

	if _, exists := p.poolSubs[mint]; exists {
		return nil // Already tracking
	}

	// Store pool address
	p.poolAddrs[mint] = poolAddr

	// Subscribe to AMM pool account for price updates
	poolSubID, err := p.client.AccountSubscribe(poolAddr, func(data json.RawMessage) {
		p.handlePoolUpdate(mint, data)
	})
	if err != nil {
		return fmt.Errorf("subscribe to pool: %w", err)
	}
	p.poolSubs[mint] = poolSubID

	log.Info().
		Str("mint", truncateStr(mint, 8)).
		Str("pool", truncateStr(poolAddr, 8)).
		Uint64("subID", poolSubID).
		Msg("tracking token via AMM pool")

	return nil
}

// TrackTokenAccount subscribes to your token account for balance updates
func (p *PriceFeed) TrackTokenAccount(mint string, tokenAccountAddr string) error {
	p.subsMu.Lock()
	defer p.subsMu.Unlock()

	if _, exists := p.tokenSubs[mint]; exists {
		return nil
	}

	subID, err := p.client.AccountSubscribe(tokenAccountAddr, func(data json.RawMessage) {
		p.handleTokenAccountUpdate(mint, data)
	})
	if err != nil {
		return fmt.Errorf("subscribe to token account: %w", err)
	}
	p.tokenSubs[mint] = subID

	log.Debug().
		Str("mint", truncateStr(mint, 8)).
		Uint64("subID", subID).
		Msg("tracking token account balance")

	return nil
}

// UntrackToken stops tracking a token
func (p *PriceFeed) UntrackToken(mint string) error {
	p.subsMu.Lock()
	defer p.subsMu.Unlock()

	// Unsubscribe pool
	if subID, exists := p.poolSubs[mint]; exists {
		delete(p.poolSubs, mint)
		p.client.Unsubscribe("accountUnsubscribe", subID)
	}

	// Unsubscribe token account
	if subID, exists := p.tokenSubs[mint]; exists {
		delete(p.tokenSubs, mint)
		p.client.Unsubscribe("accountUnsubscribe", subID)
	}

	delete(p.poolAddrs, mint)

	return nil
}

// handlePoolUpdate processes AMM pool account changes
func (p *PriceFeed) handlePoolUpdate(mint string, data json.RawMessage) {
	// Parse Raydium AMM pool data structure
	var update struct {
		Context struct {
			Slot uint64 `json:"slot"`
		} `json:"context"`
		Value struct {
			Data     []string `json:"data"` // [base64_data, "base64"]
			Lamports uint64   `json:"lamports"`
		} `json:"value"`
	}

	if err := json.Unmarshal(data, &update); err != nil {
		log.Warn().Err(err).Msg("failed to parse pool update")
		return
	}

	// For Raydium pools, we need to decode the binary data
	// Pool data layout contains base/quote reserves at specific offsets
	// This is a simplified version - actual implementation needs proper decoding

	// For now, emit a basic update to notify of pool activity
	priceUpdate := PriceUpdate{
		Mint: mint,
		Slot: update.Context.Slot,
	}

	// Get cached price or calculate from reserves
	p.pricesMu.RLock()
	if price, ok := p.prices[mint]; ok {
		priceUpdate.PriceSOL = price
	}
	p.pricesMu.RUnlock()

	p.notifyHandlers(priceUpdate)
}

// handleTokenAccountUpdate processes token account balance changes
func (p *PriceFeed) handleTokenAccountUpdate(mint string, data json.RawMessage) {
	var update struct {
		Context struct {
			Slot uint64 `json:"slot"`
		} `json:"context"`
		Value struct {
			Data struct {
				Parsed struct {
					Info struct {
						TokenAmount struct {
							Amount   string  `json:"amount"`
							Decimals int     `json:"decimals"`
							UIAmount float64 `json:"uiAmount"`
						} `json:"tokenAmount"`
					} `json:"info"`
				} `json:"parsed"`
			} `json:"data"`
		} `json:"value"`
	}

	if err := json.Unmarshal(data, &update); err != nil {
		log.Warn().Err(err).Msg("failed to parse token account update")
		return
	}

	balance, _ := strconv.ParseUint(update.Value.Data.Parsed.Info.TokenAmount.Amount, 10, 64)

	priceUpdate := PriceUpdate{
		Mint:         mint,
		TokenBalance: balance,
		Slot:         update.Context.Slot,
	}

	// Include cached price
	p.pricesMu.RLock()
	if price, ok := p.prices[mint]; ok {
		priceUpdate.PriceSOL = price
	}
	p.pricesMu.RUnlock()

	p.notifyHandlers(priceUpdate)
}

// SetPrice updates cached price (called after Jupiter quote)
func (p *PriceFeed) SetPrice(mint string, priceSOL float64) {
	p.pricesMu.Lock()
	p.prices[mint] = priceSOL
	p.pricesMu.Unlock()
}

// GetPrice returns cached price
func (p *PriceFeed) GetPrice(mint string) float64 {
	p.pricesMu.RLock()
	defer p.pricesMu.RUnlock()
	return p.prices[mint]
}

// CalculatePriceFromReserves calculates token price from AMM reserves
func CalculatePriceFromReserves(reserves PoolReserves) float64 {
	if reserves.BaseReserve == 0 {
		return 0
	}

	// Price = QuoteReserve / BaseReserve (adjusted for decimals)
	baseAmt := float64(reserves.BaseReserve) / math.Pow10(reserves.BaseDecimals)
	quoteAmt := float64(reserves.QuoteReserve) / math.Pow10(reserves.QuoteDecimals)

	return quoteAmt / baseAmt
}

// notifyHandlers calls all registered price handlers
func (p *PriceFeed) notifyHandlers(update PriceUpdate) {
	p.handlersMu.RLock()
	handlers := p.handlers
	p.handlersMu.RUnlock()

	for _, h := range handlers {
		go h(update)
	}
}

// GetTrackedCount returns number of tracked tokens
func (p *PriceFeed) GetTrackedCount() int {
	p.subsMu.RLock()
	defer p.subsMu.RUnlock()
	return len(p.poolSubs)
}

// ClearSubscriptions clears all subscription maps on disconnect
// Phase 7: Prevents duplicate subscriptions after reconnect
func (p *PriceFeed) ClearSubscriptions() {
	p.subsMu.Lock()
	defer p.subsMu.Unlock()

	// Clear all subscription IDs (they're invalid after disconnect)
	p.poolSubs = make(map[string]uint64)
	p.tokenSubs = make(map[string]uint64)

	log.Info().Msg("WebSocket subscriptions cleared (will resubscribe on reconnect)")
}

// truncateStr safely truncates a string for logging
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
