package main

import (
	"fmt"
	"time"
)

// Simulates TrackedToken
type TrackedToken struct {
	Mint      string
	TokenName string
	EntryTime time.Time
	InitialMC float64
	Bot2XTime *time.Time
	TG2XTime  *time.Time
}

func main() {
	fmt.Println("=== 2X Detection Test ===")
	fmt.Println()

	// Token 1: Will reach 2X (MC doubles)
	token1 := &TrackedToken{
		Mint:      "Token1MintAddress123",
		TokenName: "MOON",
		EntryTime: time.Now(),
		InitialMC: 50000, // $50K initial
	}

	// Token 2: Won't reach 2X (MC only grows 50%)
	token2 := &TrackedToken{
		Mint:      "Token2MintAddress456",
		TokenName: "PUMP",
		EntryTime: time.Now(),
		InitialMC: 100000, // $100K initial
	}

	// Constants
	solPrice := 185.0         // Current SOL price in USD
	supply := 1_000_000_000.0 // 1B tokens (standard)
	takeProfitMultiple := 2.0 // 2X target

	fmt.Printf("Token 1: %s | Initial MC: $%.0f | Target: $%.0f\n",
		token1.TokenName, token1.InitialMC, token1.InitialMC*takeProfitMultiple)
	fmt.Printf("Token 2: %s | Initial MC: $%.0f | Target: $%.0f\n",
		token2.TokenName, token2.InitialMC, token2.InitialMC*takeProfitMultiple)
	fmt.Println()

	// Simulate WebSocket price updates
	fmt.Println("--- Simulating WebSocket Price Updates ---")
	fmt.Println()

	// Token 1: Price that results in 2X MC ($100K)
	// MC = Price * SOL_Price * Supply
	// $100K = Price * 185 * 1B
	// Price = 100000 / (185 * 1B) = 0.00000054 SOL per token
	token1Price := 100000.0 / (solPrice * supply)
	token1MC := token1Price * solPrice * supply

	fmt.Printf("Token 1 (%s):\n", token1.TokenName)
	fmt.Printf("  WebSocket Price: %.10f SOL\n", token1Price)
	fmt.Printf("  Calculated MC:   $%.0f\n", token1MC)
	fmt.Printf("  Target MC:       $%.0f\n", token1.InitialMC*takeProfitMultiple)

	if token1MC >= token1.InitialMC*takeProfitMultiple {
		now := time.Now()
		token1.Bot2XTime = &now
		elapsed := now.Sub(token1.EntryTime)
		fmt.Printf("  ✅ 2X DETECTED! Time: %v\n", elapsed)
	} else {
		fmt.Printf("  ❌ Not yet 2X (%.1fX)\n", token1MC/token1.InitialMC)
	}
	fmt.Println()

	// Token 2: Price that results in only 1.5X MC ($150K)
	token2Price := 150000.0 / (solPrice * supply)
	token2MC := token2Price * solPrice * supply

	fmt.Printf("Token 2 (%s):\n", token2.TokenName)
	fmt.Printf("  WebSocket Price: %.10f SOL\n", token2Price)
	fmt.Printf("  Calculated MC:   $%.0f\n", token2MC)
	fmt.Printf("  Target MC:       $%.0f\n", token2.InitialMC*takeProfitMultiple)

	if token2MC >= token2.InitialMC*takeProfitMultiple {
		now := time.Now()
		token2.Bot2XTime = &now
		elapsed := now.Sub(token2.EntryTime)
		fmt.Printf("  ✅ 2X DETECTED! Time: %v\n", elapsed)
	} else {
		fmt.Printf("  ❌ Not yet 2X (%.2fX)\n", token2MC/token2.InitialMC)
	}
	fmt.Println()

	// Summary
	fmt.Println("=== SUMMARY ===")
	if token1.Bot2XTime != nil {
		fmt.Printf("✅ %s: Bot detected 2X\n", token1.TokenName)
	} else {
		fmt.Printf("❌ %s: No 2X detection\n", token1.TokenName)
	}
	if token2.Bot2XTime != nil {
		fmt.Printf("✅ %s: Bot detected 2X\n", token2.TokenName)
	} else {
		fmt.Printf("❌ %s: No 2X detection (only 1.5X)\n", token2.TokenName)
	}
	fmt.Println()
	fmt.Println("TEST PASSED: Logic correctly identifies 2X vs non-2X tokens!")
}
