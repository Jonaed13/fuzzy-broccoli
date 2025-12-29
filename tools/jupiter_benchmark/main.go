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
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	// Test parameters
	InputMint  = "So11111111111111111111111111111111111111112"  // SOL
	OutputMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v" // USDC
	Amount     = 100000000                                      // 0.1 SOL in lamports
	Iterations = 10
)

var (
	// Current endpoints (your implementation)
	MetisQuoteURL = "https://api.jup.ag/swap/v1/quote"
	MetisSwapURL  = "https://api.jup.ag/swap/v1/swap"

	// Ultra endpoints (GET for quote+tx, POST for execute)
	UltraOrderURL = "https://lite-api.jup.ag/ultra/v1/order"

	apiKey     string
	testWallet string
)

type QuoteResponse struct {
	InputMint  string `json:"inputMint"`
	OutputMint string `json:"outputMint"`
	InAmount   string `json:"inAmount"`
	OutAmount  string `json:"outAmount"`
}

type SwapRequest struct {
	QuoteResponse interface{} `json:"quoteResponse"`
	UserPublicKey string      `json:"userPublicKey"`
}

type SwapResponse struct {
	SwapTransaction string `json:"swapTransaction"`
}

type UltraOrderResponse struct {
	RequestID   string `json:"requestId"`
	Transaction string `json:"transaction"`
	InAmount    string `json:"inAmount"`
	OutAmount   string `json:"outAmount"`
}

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})

	// Load .env
	godotenv.Load()
	keys := os.Getenv("JUPITER_API_KEYS")
	if keys != "" {
		apiKey = strings.Split(keys, ",")[0]
		fmt.Printf("Using API Key: %s...%s\n", apiKey[:8], apiKey[len(apiKey)-4:])
	} else {
		fmt.Println("❌ No JUPITER_API_KEYS found in .env")
		return
	}

	// Use a real wallet for testing (needed for Ultra)
	// Load from private key
	privKey := os.Getenv("WALLET_PRIVATE_KEY")
	if privKey != "" {
		// Derive public key from private key manually
		// For now, hardcode based on your .env
		testWallet = "2fq9kYnAqrFoJAEAdWSxZ4h3MN1CWLF4vpLPjHGTz2Cv"
	} else {
		testWallet = "11111111111111111111111111111111"
	}
	fmt.Printf("Test Wallet: %s\n", testWallet)

	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║   JUPITER FULL FLOW BENCHMARK: Current (Metis) vs Ultra    ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Warmup
	fmt.Println("🔄 Warming up connections...")
	testMetisFullFlow()
	testUltraFullFlow()
	time.Sleep(1 * time.Second)

	// Benchmark Current (Quote + Swap TX)
	fmt.Println("\n━━━ TEST 1: METIS (Quote + Swap - 2 calls) ━━━")
	metisTimes := make([]time.Duration, Iterations)
	for i := 0; i < Iterations; i++ {
		start := time.Now()
		err := testMetisFullFlow()
		elapsed := time.Since(start)
		if err != nil {
			fmt.Printf("  ❌ Iteration %d: ERROR - %v\n", i+1, err)
		} else {
			metisTimes[i] = elapsed
			fmt.Printf("  ✓ Iteration %d: %dms\n", i+1, elapsed.Milliseconds())
		}
		time.Sleep(300 * time.Millisecond)
	}

	time.Sleep(2 * time.Second)

	// Benchmark Ultra (Single call for quote+tx)
	fmt.Println("\n━━━ TEST 2: ULTRA (Order - 1 call) ━━━")
	ultraTimes := make([]time.Duration, Iterations)
	for i := 0; i < Iterations; i++ {
		start := time.Now()
		err := testUltraFullFlow()
		elapsed := time.Since(start)
		if err != nil {
			fmt.Printf("  ❌ Iteration %d: ERROR - %v\n", i+1, err)
		} else {
			ultraTimes[i] = elapsed
			fmt.Printf("  ✓ Iteration %d: %dms\n", i+1, elapsed.Milliseconds())
		}
		time.Sleep(300 * time.Millisecond)
	}

	// Results
	fmt.Println("\n╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║                         RESULTS                            ║")
	fmt.Println("╠════════════════════════════════════════════════════════════╣")

	metisAvg := average(metisTimes)
	ultraAvg := average(ultraTimes)

	fmt.Printf("║ METIS (Quote+Swap):   AVG: %4dms   MED: %4dms            ║\n",
		metisAvg.Milliseconds(), median(metisTimes).Milliseconds())
	fmt.Printf("║ ULTRA (Order):        AVG: %4dms   MED: %4dms            ║\n",
		ultraAvg.Milliseconds(), median(ultraTimes).Milliseconds())

	if ultraAvg > 0 && metisAvg > 0 {
		if ultraAvg < metisAvg {
			diff := metisAvg - ultraAvg
			pct := float64(diff) / float64(metisAvg) * 100
			fmt.Printf("║ ⚡ ULTRA is %.0f%% FASTER (%dms saved per trade)         ║\n", pct, diff.Milliseconds())
		} else if metisAvg < ultraAvg {
			diff := ultraAvg - metisAvg
			pct := float64(diff) / float64(ultraAvg) * 100
			fmt.Printf("║ ✓ METIS is %.0f%% FASTER (%dms saved per trade)          ║\n", pct, diff.Milliseconds())
		} else {
			fmt.Println("║ = Both are equal speed                                     ║")
		}
	}

	fmt.Println("╚════════════════════════════════════════════════════════════╝")
}

// testMetisFullFlow - Current implementation: Quote + Swap (2 HTTP calls)
func testMetisFullFlow() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Step 1: Get Quote
	quoteURL := fmt.Sprintf("%s?inputMint=%s&outputMint=%s&amount=%d&slippageBps=50",
		MetisQuoteURL, InputMint, OutputMint, Amount)

	req, _ := http.NewRequestWithContext(ctx, "GET", quoteURL, nil)
	req.Header.Set("x-api-key", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("quote failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("quote status %d: %s", resp.StatusCode, string(body))
	}

	var quote QuoteResponse
	quoteBody, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(quoteBody, &quote); err != nil {
		return fmt.Errorf("quote decode: %w", err)
	}

	// Step 2: Get Swap Transaction
	var rawQuote interface{}
	json.Unmarshal(quoteBody, &rawQuote)

	swapReq := map[string]interface{}{
		"quoteResponse": rawQuote,
		"userPublicKey": testWallet,
	}
	swapBody, _ := json.Marshal(swapReq)

	swapHTTP, _ := http.NewRequestWithContext(ctx, "POST", MetisSwapURL, bytes.NewReader(swapBody))
	swapHTTP.Header.Set("Content-Type", "application/json")
	swapHTTP.Header.Set("x-api-key", apiKey)

	swapResp, err := http.DefaultClient.Do(swapHTTP)
	if err != nil {
		return fmt.Errorf("swap failed: %w", err)
	}
	defer swapResp.Body.Close()

	if swapResp.StatusCode != 200 {
		body, _ := io.ReadAll(swapResp.Body)
		return fmt.Errorf("swap status %d: %s", swapResp.StatusCode, string(body))
	}

	return nil
}

// testUltraFullFlow - Ultra: Order (1 HTTP call returns quote+tx)
func testUltraFullFlow() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Single call: Order (returns transaction directly)
	orderURL := fmt.Sprintf("%s?inputMint=%s&outputMint=%s&amount=%d&taker=%s",
		UltraOrderURL, InputMint, OutputMint, Amount, testWallet)

	req, _ := http.NewRequestWithContext(ctx, "GET", orderURL, nil)
	req.Header.Set("x-api-key", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("order failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("order status %d: %s", resp.StatusCode, string(body))
	}

	var order UltraOrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&order); err != nil {
		return fmt.Errorf("order decode: %w", err)
	}

	// Note: Transaction may be empty if wallet has insufficient funds
	// but we still measure API response time if we got a requestId
	if order.RequestID == "" {
		return fmt.Errorf("no requestId in response")
	}

	return nil
}

func average(times []time.Duration) time.Duration {
	var total time.Duration
	count := 0
	for _, t := range times {
		if t > 0 {
			total += t
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return total / time.Duration(count)
}

func median(times []time.Duration) time.Duration {
	valid := make([]time.Duration, 0)
	for _, t := range times {
		if t > 0 {
			valid = append(valid, t)
		}
	}
	if len(valid) == 0 {
		return 0
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i] < valid[j] })
	return valid[len(valid)/2]
}
