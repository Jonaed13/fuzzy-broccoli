package jupiter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/net/http2"
)

// Metis API endpoint (new, faster)
const MetisSwapURL = "https://api.jup.ag/swap/v1"

// Client handles Jupiter Metis API calls with HTTP/2 pooling and API key rotation
type Client struct {
	baseURL     string
	slippageBps int
	clientPool  *HTTPClientPool
	apiKeys     []string
	keyIdx      atomic.Uint32
	maxLamports uint64 // Max priority fee cap
	
	// Simulation
	simMode       bool
	simMultiplier float64
	simMu         sync.RWMutex
}

// DefaultAPIKeys returns fallback API keys (should use env vars in production)
func DefaultAPIKeys() []string {
	return []string{
		"public-key", // Fallback - use JUPITER_API_KEYS env var
	}
}

// HTTPClientPool provides HTTP/2 connection pooling
type HTTPClientPool struct {
	clients []*http.Client
	mu      sync.Mutex
	idx     uint32
}

// NewHTTPClientPool creates an HTTP/2 optimized client pool
func NewHTTPClientPool(size int, timeout time.Duration) *HTTPClientPool {
	pool := &HTTPClientPool{
		clients: make([]*http.Client, size),
	}

	for i := 0; i < size; i++ {
		transport := &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     90 * time.Second,
			ForceAttemptHTTP2:   true,
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}

		http2.ConfigureTransport(transport)

		pool.clients[i] = &http.Client{
			Transport: transport,
			Timeout:   timeout,
		}
	}

	log.Info().Int("poolSize", size).Msg("HTTP/2 client pool initialized")
	return pool
}

func (p *HTTPClientPool) Get() *http.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	client := p.clients[p.idx%uint32(len(p.clients))]
	p.idx++
	return client
}

// NewClient creates a Jupiter Metis API client
func NewClient(baseURL string, slippageBps int, timeout time.Duration) *Client {
	return NewClientWithKeys(baseURL, slippageBps, timeout, nil)
}

// NewClientWithKeys creates a Jupiter client with custom API keys
func NewClientWithKeys(baseURL string, slippageBps int, timeout time.Duration, apiKeys []string) *Client {
	// Load API keys from environment if not provided
	if len(apiKeys) == 0 {
		if envKeys := os.Getenv("JUPITER_API_KEYS"); envKeys != "" {
			apiKeys = strings.Split(envKeys, ",")
		} else {
			apiKeys = DefaultAPIKeys()
		}
	}
	
	return &Client{
		baseURL:       MetisSwapURL, // Use Metis endpoint
		slippageBps:   slippageBps,
		clientPool:    NewHTTPClientPool(4, timeout),
		apiKeys:       apiKeys,
		maxLamports:   1_250_000,
		simMultiplier: 1.0,
	}
}

// SetSimulation configures the simulation mode
func (c *Client) SetSimulation(enabled bool, multiplier float64) {
	c.simMu.Lock()
	defer c.simMu.Unlock()
	c.simMode = enabled
	c.simMultiplier = multiplier
	log.Info().Bool("enabled", enabled).Float64("mult", multiplier).Msg("Jupiter Simulation Mode Configured")
}

// getAPIKey returns next API key (round-robin)
func (c *Client) getAPIKey() string {
	idx := c.keyIdx.Add(1) % uint32(len(c.apiKeys))
	return c.apiKeys[idx]
}

// QuoteResponse from Jupiter
type QuoteResponse struct {
	InputMint            string          `json:"inputMint"`
	InAmount             string          `json:"inAmount"`
	OutputMint           string          `json:"outputMint"`
	OutAmount            string          `json:"outAmount"`
	OtherAmountThreshold string          `json:"otherAmountThreshold"`
	SwapMode             string          `json:"swapMode"`
	SlippageBps          int             `json:"slippageBps"`
	PriceImpactPct       string          `json:"priceImpactPct"`
	RoutePlan            []RoutePlanStep `json:"routePlan"`
	ContextSlot          uint64          `json:"contextSlot"`
	TimeTaken            float64         `json:"timeTaken"`
}

type RoutePlanStep struct {
	SwapInfo SwapInfo `json:"swapInfo"`
	Percent  int      `json:"percent"`
}

type SwapInfo struct {
	AmmKey     string `json:"ammKey"`
	Label      string `json:"label"`
	InputMint  string `json:"inputMint"`
	OutputMint string `json:"outputMint"`
	InAmount   string `json:"inAmount"`
	OutAmount  string `json:"outAmount"`
	FeeAmount  string `json:"feeAmount"`
	FeeMint    string `json:"feeMint"`
}

// SwapResponse from Jupiter Metis
type SwapResponse struct {
	SwapTransaction          string `json:"swapTransaction"`
	LastValidBlockHeight     uint64 `json:"lastValidBlockHeight"`
	PrioritizationFeeLamports uint64 `json:"prioritizationFeeLamports"`
}

// PriorityLevelWithMaxLamports for dynamic fee estimation
type PriorityLevelWithMaxLamports struct {
	PriorityLevelWithMaxLamports struct {
		PriorityLevel string `json:"priorityLevel"` // medium, high, veryHigh
		MaxLamports   uint64 `json:"maxLamports"`
		Global        bool   `json:"global,omitempty"`
	} `json:"priorityLevelWithMaxLamports"`
}

// GetQuote fetches a swap quote from Jupiter
func (c *Client) GetQuote(ctx context.Context, inputMint, outputMint string, amountLamports uint64) (*QuoteResponse, error) {
	// Simulation Interceptor
	c.simMu.RLock()
	isSim := c.simMode
	mult := c.simMultiplier
	c.simMu.RUnlock()
	
	if isSim {
		// Mock logic: return amount * multiplier
		// If input is SOL (SOLMint), we are buying -> return output (Tokens) * multiplier?
		// Usually price is determined by market.
		// For our test: 
		// "Assume random coin reached 50%" calls GetQuote? No, Telegram signal provides price.
		// Monitoring loop calls GetQuote to check value of HELD TOKENS (Input=Token, Output=SOL).
		
		// If Input != SOLMint (Selling/Checking Value):
		if inputMint != "So11111111111111111111111111111111111111112" {
			// Calculate Mock Output (SOL)
			// Assume 1:1 base price * multiplier
			outAmt := float64(amountLamports) * mult
			return &QuoteResponse{
				InputMint: inputMint,
				InAmount: fmt.Sprintf("%d", amountLamports),
				OutputMint: outputMint,
				OutAmount: fmt.Sprintf("%.0f", outAmt),
				PriceImpactPct: "0.0",
			}, nil
		} else {
			// Buying (SOL -> Token)
			// Assume 1 SOL = 1 Token (adjusted for decimals temporarily) or just pass through amount
			// But since signals usually are small amounts, let's say 1 SOL = 100 FakeTokens
			// Multiplier usually applies to PRICE. If price is 2.5X, buying gives FEWER tokens.
			// But for simulation simplicity:
			// Just return OutAmount = InAmount. (1:1)
			outAmt := amountLamports 
			return &QuoteResponse{
				InputMint: inputMint,
				InAmount: fmt.Sprintf("%d", amountLamports),
				OutputMint: outputMint,
				OutAmount: fmt.Sprintf("%d", outAmt),
				PriceImpactPct: "0.0",
			}, nil
		}
	}

	start := time.Now()

	url := fmt.Sprintf("%s/quote?inputMint=%s&outputMint=%s&amount=%d&slippageBps=%d",
		c.baseURL, inputMint, outputMint, amountLamports, c.slippageBps)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-api-key", c.getAPIKey())

	client := c.clientPool.Get()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("quote failed (%d): %s", resp.StatusCode, string(body))
	}

	var quote QuoteResponse
	// Optimized: Use Decoder to stream response
	if err := json.NewDecoder(resp.Body).Decode(&quote); err != nil {
		return nil, fmt.Errorf("decode quote: %w", err)
	}

	log.Debug().
		Dur("latency", time.Since(start)).
		Str("outAmount", quote.OutAmount).
		Msg("jupiter quote")

	return &quote, nil
}

// GetSwapTransaction fetches swap TX using Jupiter Metis API with veryHigh priority
func (c *Client) GetSwapTransaction(ctx context.Context, inputMint, outputMint, userPubkey string, amountLamports uint64) (string, error) {
	start := time.Now()

	// Get quote first
	quote, err := c.GetQuote(ctx, inputMint, outputMint, amountLamports)
	if err != nil {
		return "", fmt.Errorf("get quote: %w", err)
	}

	quoteLatency := time.Since(start)

	// Build swap request with dynamic priority fee (veryHigh with cap)
	reqBody := struct {
		QuoteResponse             *QuoteResponse                `json:"quoteResponse"`
		UserPublicKey             string                        `json:"userPublicKey"`
		WrapAndUnwrapSol          bool                          `json:"wrapAndUnwrapSol"`
		DynamicComputeUnitLimit   bool                          `json:"dynamicComputeUnitLimit"`
		SkipUserAccountsRpcCalls  bool                          `json:"skipUserAccountsRpcCalls"`
		PrioritizationFeeLamports *PriorityLevelWithMaxLamports `json:"prioritizationFeeLamports"`
	}{
		QuoteResponse:            quote,
		UserPublicKey:            userPubkey,
		WrapAndUnwrapSol:         true,
		DynamicComputeUnitLimit:  true,  // Let Jupiter optimize compute units
		SkipUserAccountsRpcCalls: true,  // Speed optimization
		PrioritizationFeeLamports: &PriorityLevelWithMaxLamports{
			PriorityLevelWithMaxLamports: struct {
				PriorityLevel string `json:"priorityLevel"`
				MaxLamports   uint64 `json:"maxLamports"`
				Global        bool   `json:"global,omitempty"`
			}{
				PriorityLevel: "veryHigh", // Maximum priority
				MaxLamports:   c.maxLamports,
				Global:        false, // Local fee market (more accurate)
			},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/swap", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-api-key", c.getAPIKey())

	client := c.clientPool.Get()
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("swap failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var swapResp SwapResponse
	// Optimized: Use Decoder to stream response
	if err := json.NewDecoder(resp.Body).Decode(&swapResp); err != nil {
		return "", fmt.Errorf("decode swap response: %w", err)
	}

	totalLatency := time.Since(start)
	swapLatency := totalLatency - quoteLatency

	log.Info().
		Dur("quoteLatency", quoteLatency).
		Dur("swapLatency", swapLatency).
		Dur("totalLatency", totalLatency).
		Uint64("priorityFee", swapResp.PrioritizationFeeLamports).
		Msg("jupiter swap tx")

	return swapResp.SwapTransaction, nil
}

// SetMaxPriorityFee sets the max priority fee cap in lamports
func (c *Client) SetMaxPriorityFee(lamports uint64) {
	c.maxLamports = lamports
}

// SOL mint address constant
const SOLMint = "So11111111111111111111111111111111111111112"
