package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/joho/godotenv"
)

// Jupiter Metis API
const JupiterSwapURL = "https://api.jup.ag/swap/v1"
const SOLMint = "So11111111111111111111111111111111111111112"

// Popular token for testing (BONK - high liquidity)
const TestTokenMint = "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263"

type QuoteResponse struct {
	InputMint  string  `json:"inputMint"`
	InAmount   string  `json:"inAmount"`
	OutputMint string  `json:"outputMint"`
	OutAmount  string  `json:"outAmount"`
	RoutePlan  []any   `json:"routePlan"`
	TimeTaken  float64 `json:"timeTaken"`
}

type SwapResponse struct {
	SwapTransaction string `json:"swapTransaction"`
}

type BenchResult struct {
	QuoteMs int64
	SwapMs  int64
	TotalMs int64
	Error   string
}

func main() {
	// Load .env for API keys
	godotenv.Load("../../.env")
	apiKey := os.Getenv("JUPITER_API_KEYS")
	if apiKey == "" {
		apiKey = "demo" // Fallback
	}
	// Use first key if multiple
	if idx := len(apiKey); idx > 36 {
		apiKey = apiKey[:36]
	}

	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Println("       JUPITER TRADING SPEED BENCHMARK")
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Printf("Testing: SOL → BONK (0.001 SOL)\n")
	fmt.Printf("API Key: %s...\n\n", apiKey[:8])

	// Test parameters
	iterations := 10
	amountLamports := uint64(1_000_000) // 0.001 SOL

	results := make([]BenchResult, 0, iterations)

	fmt.Println("Running benchmark...")
	fmt.Println()

	for i := 0; i < iterations; i++ {
		result := runBenchmark(apiKey, amountLamports)
		results = append(results, result)

		if result.Error != "" {
			fmt.Printf("  [%d/%d] ❌ ERROR: %s\n", i+1, iterations, result.Error)
		} else {
			fmt.Printf("  [%d/%d] ✅ Quote: %dms | Swap TX: %dms | TOTAL: %dms\n",
				i+1, iterations, result.QuoteMs, result.SwapMs, result.TotalMs)
		}

		// Small delay between requests to avoid rate limiting
		time.Sleep(500 * time.Millisecond)
	}

	// Calculate statistics
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Println("                   RESULTS")
	fmt.Println("═══════════════════════════════════════════════════")

	var successResults []BenchResult
	for _, r := range results {
		if r.Error == "" {
			successResults = append(successResults, r)
		}
	}

	if len(successResults) == 0 {
		fmt.Println("❌ All requests failed!")
		return
	}

	// Sort by total time
	sort.Slice(successResults, func(i, j int) bool {
		return successResults[i].TotalMs < successResults[j].TotalMs
	})

	// Calculate p50 (median)
	p50Idx := len(successResults) / 2
	p50 := successResults[p50Idx]

	// Calculate average
	var totalQuote, totalSwap, totalTotal int64
	for _, r := range successResults {
		totalQuote += r.QuoteMs
		totalSwap += r.SwapMs
		totalTotal += r.TotalMs
	}
	avgQuote := totalQuote / int64(len(successResults))
	avgSwap := totalSwap / int64(len(successResults))
	avgTotal := totalTotal / int64(len(successResults))

	// Min/Max
	minResult := successResults[0]
	maxResult := successResults[len(successResults)-1]

	fmt.Printf("Success Rate: %d/%d (%.0f%%)\n\n", len(successResults), iterations,
		float64(len(successResults))/float64(iterations)*100)

	fmt.Println("┌────────────┬──────────┬──────────┬──────────┐")
	fmt.Println("│  Metric    │  Quote   │  Swap TX │  TOTAL   │")
	fmt.Println("├────────────┼──────────┼──────────┼──────────┤")
	fmt.Printf("│  P50       │  %4dms  │  %4dms  │  %4dms  │\n", p50.QuoteMs, p50.SwapMs, p50.TotalMs)
	fmt.Printf("│  Average   │  %4dms  │  %4dms  │  %4dms  │\n", avgQuote, avgSwap, avgTotal)
	fmt.Printf("│  Min       │  %4dms  │  %4dms  │  %4dms  │\n", minResult.QuoteMs, minResult.SwapMs, minResult.TotalMs)
	fmt.Printf("│  Max       │  %4dms  │  %4dms  │  %4dms  │\n", maxResult.QuoteMs, maxResult.SwapMs, maxResult.TotalMs)
	fmt.Println("└────────────┴──────────┴──────────┴──────────┘")

	fmt.Println()
	fmt.Println("Note: This measures Jupiter API latency only.")
	fmt.Println("      Actual trade includes: Parse + Sign + RPC Send (~50-100ms more)")
}

func runBenchmark(apiKey string, amountLamports uint64) BenchResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := BenchResult{}
	overallStart := time.Now()

	// Step 1: Get Quote
	quoteStart := time.Now()
	quote, err := getQuote(ctx, apiKey, SOLMint, TestTokenMint, amountLamports)
	result.QuoteMs = time.Since(quoteStart).Milliseconds()

	if err != nil {
		result.Error = fmt.Sprintf("quote: %v", err)
		result.TotalMs = time.Since(overallStart).Milliseconds()
		return result
	}

	// Step 2: Get Swap Transaction
	swapStart := time.Now()
	_, err = getSwapTransaction(ctx, apiKey, quote)
	result.SwapMs = time.Since(swapStart).Milliseconds()

	if err != nil {
		result.Error = fmt.Sprintf("swap: %v", err)
		result.TotalMs = time.Since(overallStart).Milliseconds()
		return result
	}

	result.TotalMs = time.Since(overallStart).Milliseconds()
	return result
}

func getQuote(ctx context.Context, apiKey, inputMint, outputMint string, amount uint64) (json.RawMessage, error) {
	url := fmt.Sprintf("%s/quote?inputMint=%s&outputMint=%s&amount=%d&slippageBps=100",
		JupiterSwapURL, inputMint, outputMint, amount)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	// Return raw JSON to pass to swap endpoint
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return json.RawMessage(body), nil
}

func getSwapTransaction(ctx context.Context, apiKey string, quoteRaw json.RawMessage) (string, error) {
	// Use a dummy wallet address (we're just benchmarking, not executing)
	dummyWallet := "11111111111111111111111111111111"

	reqBody := map[string]interface{}{
		"quoteResponse":            json.RawMessage(quoteRaw),
		"userPublicKey":            dummyWallet,
		"wrapAndUnwrapSol":         true,
		"dynamicComputeUnitLimit":  true,
		"skipUserAccountsRpcCalls": true,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", JupiterSwapURL+"/swap", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var swapResp SwapResponse
	if err := json.NewDecoder(resp.Body).Decode(&swapResp); err != nil {
		return "", err
	}

	return swapResp.SwapTransaction, nil
}
