package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	signalPkg "solana-pump-bot/internal/signal"
)

func main() {
	// Read input from stdin
	reader := bufio.NewReader(os.Stdin)
	text, _ := reader.ReadString('\x00') // Read until EOF
	text = strings.TrimSpace(text)

	if text == "" {
		// Fallback to args
		if len(os.Args) > 1 {
			text = strings.Join(os.Args[1:], " ")
		} else {
			color.Red("❌ No input text provided")
			os.Exit(1)
		}
	}

	fmt.Println("----------------------------------------")
	fmt.Println("🔍 ANALYZING SIGNAL")
	fmt.Println("----------------------------------------")
	fmt.Printf("Input: %s\n\n", text)

	parser := signalPkg.NewParser()
	sig, err := parser.Parse(text, 0)
	if err != nil {
		color.Red("❌ Parse Error: %v", err)
		os.Exit(1)
	}

	if sig == nil {
		color.Yellow("⚠️  No signal pattern found")
		os.Exit(0)
	}

	// Classify
	parser.Classify(sig, 50.0, 2.0) // Hardcoded 50% entry, 2x exit for testing

	fmt.Println("✅ SIGNAL FOUND")
	fmt.Printf("Token: %s\n", sig.TokenName)
	fmt.Printf("Value: %.2f %s\n", sig.Value, sig.Unit)
	fmt.Printf("Type:  %s\n", sig.Type)
	
	if sig.Mint != "" {
		fmt.Printf("CA:    %s\n", sig.Mint)
	} else {
		fmt.Printf("CA:    (Not found in message)\n")
	}

	fmt.Println("----------------------------------------")
	if sig.Type == signalPkg.SignalEntry {
		color.Green("🎯 MATCH: 50% UP SIGNAL DETECTED")
	} else if sig.Type == signalPkg.SignalExit {
		color.Blue("🚀 MATCH: 2X EXIT SIGNAL DETECTED")
	} else {
		color.Red("❌ NO MATCH: Signal found but did not meet criteria")
	}
}
