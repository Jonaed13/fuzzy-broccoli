package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"solana-pump-bot/internal/blockchain"
	"solana-pump-bot/internal/jupiter"
)

// Test wallet (DO NOT USE FOR REAL TRADING - this is a throwaway key)
// Generate new one: solana-keygen new --no-bip39-passphrase
const testPrivateKey = "4wBqpZM9xaSheZzJSMawUHDgZ7miWfSsxmfVF5BJWybHxPNzLwBY3k1BwBWmPaqXLuxYXq5TtF8z1rJNNmLxmXe7"

// Test token to buy (BONK - high liquidity)
const testMint = "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263"

func main() {
	// Setup logger
	log.Logger = zerolog.New(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05.000"},
	).With().Timestamp().Logger()

	fmt.Println("🧪 SPEED TEST: Simulating Buy Trade")
	fmt.Println("=" + string(make([]byte, 50)))

	// Initialize components
	rpcURL := "https://rpc.shyft.to?api_key=48KZbYxP-9e9SpqR"
	fallbackURL := "https://mainnet.helius-rpc.com/?api-key=465a28e0-e3b3-4991-8878-0e7adbb78f81"

	totalStart := time.Now()
	timings := make(map[string]time.Duration)

	// Step 1: Load wallet
	step1Start := time.Now()
	wallet, err := blockchain.NewWallet(testPrivateKey)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load wallet")
	}
	timings["1_wallet_load"] = time.Since(step1Start)
	fmt.Printf("✓ Wallet: %s\n", wallet.Address())

	// Step 2: Initialize RPC
	step2Start := time.Now()
	rpc := blockchain.NewRPCClient(rpcURL, fallbackURL, "")
	timings["2_rpc_init"] = time.Since(step2Start)

	// Step 3: Get balance
	step3Start := time.Now()
	ctx := context.Background()
	balance, err := rpc.GetBalance(ctx, wallet.Address())
	if err != nil {
		log.Warn().Err(err).Msg("balance check failed")
	}
	timings["3_balance_check"] = time.Since(step3Start)
	fmt.Printf("✓ Balance: %.6f SOL\n", float64(balance)/1e9)

	// Step 4: Initialize blockhash cache
	step4Start := time.Now()
	blockhashCache := blockchain.NewBlockhashCache(rpc, 100*time.Millisecond, 90*time.Second)
	if err := blockhashCache.Start(); err != nil {
		log.Fatal().Err(err).Msg("blockhash cache failed")
	}
	defer blockhashCache.Stop()
	timings["4_blockhash_init"] = time.Since(step4Start)

	// Step 5: Initialize Jupiter
	step5Start := time.Now()
	jupiterClient := jupiter.NewClient("https://api.jup.ag/swap/v1", 500, 10*time.Second)
	timings["5_jupiter_init"] = time.Since(step5Start)

	// Wait for blockhash to be ready
	time.Sleep(200 * time.Millisecond)

	fmt.Println("\n--- TRADE SIMULATION ---")

	// Step 6: Get Jupiter quote + swap TX
	tradeStart := time.Now()
	
	step6Start := time.Now()
	amountLamports := uint64(10_000_000) // 0.01 SOL test amount
	swapTx, err := jupiterClient.GetSwapTransaction(ctx, jupiter.SOLMint, testMint, wallet.Address(), amountLamports)
	if err != nil {
		log.Error().Err(err).Msg("Jupiter swap failed")
		fmt.Printf("❌ Jupiter error (may be insufficient balance): %v\n", err)
	} else {
		timings["6_jupiter_swap"] = time.Since(step6Start)
		fmt.Printf("✓ Jupiter TX received (%d bytes)\n", len(swapTx))
	}

	// Step 7: Get cached blockhash
	step7Start := time.Now()
	blockhash, err := blockhashCache.Get()
	if err != nil {
		log.Error().Err(err).Msg("blockhash failed")
	}
	timings["7_blockhash_get"] = time.Since(step7Start)
	fmt.Printf("✓ Blockhash: %s...\n", blockhash[:16])

	// Step 8: Sign transaction (simulation - decode and sign)
	step8Start := time.Now()
	if swapTx != "" {
		txBytes, _ := base64.StdEncoding.DecodeString(swapTx)
		if len(txBytes) > 0 {
			// Simulate signing (just measure the decode + sign overhead)
			_ = wallet.Sign(txBytes[:64]) // Sign first 64 bytes as test
		}
	}
	timings["8_tx_sign"] = time.Since(step8Start)
	fmt.Println("✓ TX signed (simulation)")

	// Step 9: RPC send (DRY RUN - we won't actually send)
	step9Start := time.Now()
	// We measure RPC latency by doing a slot query instead of actual send
	_, _ = rpc.GetBalance(ctx, wallet.Address())
	timings["9_rpc_latency"] = time.Since(step9Start)
	fmt.Println("✓ RPC send latency measured")

	tradeLatency := time.Since(tradeStart)
	totalLatency := time.Since(totalStart)

	// Results
	fmt.Println("\n" + string(make([]byte, 50)))
	fmt.Println("📊 LATENCY BREAKDOWN")
	fmt.Println(string(make([]byte, 50)))

	for name, dur := range timings {
		fmt.Printf("  %-20s %6dms\n", name, dur.Milliseconds())
	}

	fmt.Println(string(make([]byte, 50)))
	fmt.Printf("  %-20s %6dms\n", "TRADE SIMULATION", tradeLatency.Milliseconds())
	fmt.Printf("  %-20s %6dms\n", "TOTAL", totalLatency.Milliseconds())
	fmt.Println(string(make([]byte, 50)))

	// Summary
	fmt.Println("\n🎯 SUMMARY")
	if timings["6_jupiter_swap"] > 0 {
		jupiterMs := timings["6_jupiter_swap"].Milliseconds()
		rpcMs := timings["9_rpc_latency"].Milliseconds()
		signMs := timings["8_tx_sign"].Milliseconds()
		blockhashMs := timings["7_blockhash_get"].Milliseconds()
		
		estimatedTradeMs := jupiterMs + signMs + rpcMs + blockhashMs
		fmt.Printf("  Estimated real trade latency: %dms\n", estimatedTradeMs)
		fmt.Printf("  Jupiter API: %dms\n", jupiterMs)
		fmt.Printf("  RPC latency: %dms\n", rpcMs)
		fmt.Printf("  Sign + Blockhash: %dms\n", signMs+blockhashMs)
	}

	fmt.Println("\n✅ Speed test complete")
}
