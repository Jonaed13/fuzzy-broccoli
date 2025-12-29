package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Simplified from internal/signal/parser.go
type Signal struct {
	TokenName string
	Value     float64
	Unit      string
	Mint      string
}

func main() {
	// User's exact text (with potential hidden chars)
	text := `📈 ‎POORWHALE (https://www.geckoterminal.com/solana/tokens/A914FxTY8cCXMaX17AjcteUR3GdXGZKeeb3hX6wPpump?embed=1&info=0&swaps=0&grayscale=0&light_chart=0&chart_type=market_cap&resolution=1m) is up 53% 📈`

	fmt.Printf("Analyzing text: %q\n", text)

	// 1. Clean invisible chars
	cleanText := strings.Map(func(r rune) rune {
		if r == '\u200e' || r == '\u200f' || r == '\u200b' {
			return -1 // Delete LTRM, RTLM, ZWSP
		}
		return r
	}, text)

	fmt.Printf("Cleaned text: %q\n", cleanText)

	// 2. Extracts CA (logic from parser.go)
	caPattern := regexp.MustCompile(`geckoterminal\.com/solana/tokens/([1-9A-HJ-NP-Za-km-z]{32,44})`)
	caMatches := caPattern.FindStringSubmatch(cleanText)
	var mint string
	if len(caMatches) >= 2 {
		mint = caMatches[1]
		fmt.Printf("✅ Found Mint: %s\n", mint)
	} else {
		fmt.Println("❌ No Mint found")
	}

	// 3. Extract Signal (logic from parser.go)
	// Original regex: `📈\s*([A-Z0-9]+)\s+is\s+up\s+([0-9.]+)\s*(%|X)\s*📈`
	// Note: The user's text has the CA URL *inside* the pattern!
	// "POORWHALE (url...) is up 53%"
	// The original regex expects "POORWHALE is up 53%"

	pattern := regexp.MustCompile(`📈\s*([A-Z0-9]+)\s+is\s+up\s+([0-9.]+)\s*(%|X)\s*📈`)
	matches := pattern.FindStringSubmatch(cleanText)

	if len(matches) == 4 {
		fmt.Printf("✅ MATCH! Token: %s, Value: %s, Unit: %s\n", matches[1], matches[2], matches[3])
	} else {
		fmt.Println("❌ NO MATCH with strict pattern")

		// Let's try to debug why
		// The text "POORWHALE (https://...) is up 53%" contains the URL between TokenName and "is up"
		// The regex `📈\s*([A-Z0-9]+)\s+is\s+up` fails because it doesn't allow for the URL part.
	}

	// Proposed fix: Allow random text (like URLs) between Token and "is up"
	// New regex: `📈\s*([A-Z0-9]+).*?is\s+up\s+([0-9.]+)\s*(%|X)\s*📈`
	fmt.Println("\nTesting flexible pattern...")
	flexPattern := regexp.MustCompile(`📈\s*([A-Z0-9]+).*?is\s+up\s+([0-9.]+)\s*(%|X)\s*📈`)
	flexMatches := flexPattern.FindStringSubmatch(cleanText)

	if len(flexMatches) == 4 {
		fmt.Printf("✅ FLEX MATCH! Token: %s, Value: %s, Unit: %s\n", flexMatches[1], flexMatches[2], flexMatches[3])
	} else {
		fmt.Println("❌ FLEX MATCH FAILED")
	}
}
