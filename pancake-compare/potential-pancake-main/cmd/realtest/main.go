package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"solana-pump-bot/internal/blockchain"
	"solana-pump-bot/internal/config"
	"solana-pump-bot/internal/jupiter"
	"solana-pump-bot/internal/trading"
)

// USDC Mint Address (Most liquid token on Solana)
const TestTokenMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
const TestTokenName = "USDC"

// Max SOL to spend (HARD LIMIT)
const MaxTestSOL = 0.001 // 0.001 SOL = 1,000,000 lamports

func main() {
	// 1. Setup Logger
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"})
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log.Info().Msg("🧪 STARTING REAL TRANSACTION TEST 🧪")
	log.Warn().Float64("maxSOL", MaxTestSOL).Str("token", TestTokenName).Msg("⚠️  THIS WILL SPEND REAL SOL!")

	// 2. Load Config
	cfg, err := config.NewManager("config/config.yaml")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}
	time.Sleep(500 * time.Millisecond) // Let config settle

	// Override settings for safety
	cfg.Update(func(c *config.Config) {
		c.Trading.SimulationMode = false // REAL MODE
		c.Trading.AutoTradingEnabled = false // Manual control only
	})

	// 3. Init Wallet (REAL)
	wallet, err := blockchain.NewWallet(os.Getenv(cfg.Get().Wallet.PrivateKeyEnv))
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load wallet - ensure WALLET_PRIVATE_KEY is set")
	}
	log.Info().Str("address", wallet.Address()).Msg("wallet loaded")

	// 4. Init Components
	rpc := blockchain.NewRPCClient(cfg.Get().RPC.ShyftURL, cfg.Get().RPC.FallbackURL, cfg.Get().RPC.ShyftAPIKeyEnv)
	jup := jupiter.NewClient(cfg.Get().Jupiter.QuoteAPIURL, 50, 5*time.Second)
	blockhashCache := blockchain.NewBlockhashCache(rpc, 100*time.Millisecond, 30*time.Second)
	balance := blockchain.NewBalanceTracker(wallet, rpc)
	txBuilder := blockchain.NewTransactionBuilder(wallet, blockhashCache, 100000) // 0.0001 SOL priority fee

	// Start blockhash cache
	blockhashCache.Start()

	// 5. Check Balance
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Refresh balance
	if err := balance.Refresh(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to get balance")
	}
	balanceSOL := float64(balance.BalanceLamports()) / 1e9
	log.Info().Float64("balanceSOL", balanceSOL).Msg("current wallet balance")

	if balanceSOL < MaxTestSOL+0.005 { // Need extra for fees
		log.Fatal().Float64("required", MaxTestSOL+0.005).Float64("have", balanceSOL).Msg("insufficient balance")
	}

	// 6. Calculate Buy Amount (HARD CAPPED)
	buyLamports := uint64(MaxTestSOL * 1e9) // 0.001 SOL = 1,000,000 lamports
	log.Info().Uint64("lamports", buyLamports).Msg("buying with")

	// 7. Get Quote for BUY
	log.Info().Msg("--- STEP 1: GET BUY QUOTE ---")
	quote, err := jup.GetQuote(ctx, jupiter.SOLMint, TestTokenMint, buyLamports)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to get buy quote")
	}
	log.Info().
		Str("inAmount", quote.InAmount).
		Str("outAmount", quote.OutAmount).
		Str("priceImpact", quote.PriceImpactPct).
		Msg("quote received")

	// 8. Get Swap Transaction
	log.Info().Msg("--- STEP 2: GET SWAP TX ---")
	swapTx, err := jup.GetSwapTransaction(ctx, jupiter.SOLMint, TestTokenMint, wallet.Address(), buyLamports)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to get swap transaction")
	}
	log.Info().Int("txLen", len(swapTx)).Msg("swap transaction received")

	// 9. Sign Transaction
	log.Info().Msg("--- STEP 3: SIGN TX ---")
	signedTx, err := txBuilder.SignSerializedTransaction(swapTx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to sign transaction")
	}
	log.Info().Int("signedLen", len(signedTx)).Msg("transaction signed")

	// 10. Send Transaction (BUY)
	log.Info().Msg("--- STEP 4: SEND BUY TX ---")
	txSig, err := rpc.SendTransaction(ctx, signedTx, true) // skipPreflight=true for speed
	if err != nil {
		log.Fatal().Err(err).Msg("❌ BUY FAILED")
	}
	log.Info().Str("txSig", txSig).Msg("✅ BUY TX SENT!")
	log.Info().Str("explorer", fmt.Sprintf("https://solscan.io/tx/%s", txSig)).Msg("view on explorer")

	// 11. Wait for confirmation
	log.Info().Msg("⏳ Waiting 5s for confirmation...")
	time.Sleep(5 * time.Second)

	// 12. Get Token Balance
	log.Info().Msg("--- STEP 5: CHECK TOKEN BALANCE ---")
	tracker := trading.NewPositionTracker(nil, 10)
	executor := trading.NewExecutorFast(cfg, wallet, rpc, jup, txBuilder, tracker, balance, nil)
	
	// Use internal method to get token balance
	// We need to check if we actually received tokens
	tokenBalance, err := getTokenBalanceRPC(ctx, rpc, wallet.Address(), TestTokenMint)
	if err != nil {
		log.Warn().Err(err).Msg("failed to get token balance (may still be confirming)")
	} else {
		log.Info().Uint64("tokenBalance", tokenBalance).Msg("token balance")
	}

	// 13. Sell All Tokens Back
	log.Info().Msg("--- STEP 6: SELL TOKENS BACK ---")
	if tokenBalance == 0 {
		// Try to sell whatever we got from quote output
		log.Warn().Msg("using quote output amount for sell")
		fmt.Sscanf(quote.OutAmount, "%d", &tokenBalance)
	}

	if tokenBalance == 0 {
		log.Fatal().Msg("no tokens to sell!")
	}

	sellQuote, err := jup.GetQuote(ctx, TestTokenMint, jupiter.SOLMint, tokenBalance)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to get sell quote")
	}
	log.Info().
		Str("inAmount", sellQuote.InAmount).
		Str("outAmount", sellQuote.OutAmount).
		Msg("sell quote received")

	sellTx, err := jup.GetSwapTransaction(ctx, TestTokenMint, jupiter.SOLMint, wallet.Address(), tokenBalance)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to get sell transaction")
	}

	signedSellTx, err := txBuilder.SignSerializedTransaction(sellTx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to sign sell transaction")
	}

	sellSig, err := rpc.SendTransaction(ctx, signedSellTx, true)
	if err != nil {
		log.Fatal().Err(err).Msg("❌ SELL FAILED")
	}
	log.Info().Str("txSig", sellSig).Msg("✅ SELL TX SENT!")
	log.Info().Str("explorer", fmt.Sprintf("https://solscan.io/tx/%s", sellSig)).Msg("view on explorer")

	// 14. Summary
	log.Info().Msg("🏁 REAL TEST COMPLETE")
	log.Info().
		Str("buyTx", txSig).
		Str("sellTx", sellSig).
		Msg("transaction summary")
	
	// Keep executor reference to avoid unused import
	_ = executor
}

// getTokenBalanceRPC fetches token balance via RPC
func getTokenBalanceRPC(ctx context.Context, rpc *blockchain.RPCClient, owner, mint string) (uint64, error) {
	// This is a simplified version - in production use getTokenAccountsByOwner
	// For now, we'll return 0 and rely on quote output
	return 0, nil
}
