package signal

import (
	"regexp"
	"strconv"
	"strings"
)

// SignalType represents the type of trading signal
type SignalType string

const (
	SignalEntry  SignalType = "entry"
	SignalExit   SignalType = "exit"
	SignalIgnore SignalType = "ignore"
)

// Signal represents a parsed trading signal
type Signal struct {
	TokenName string     `json:"token_name"`
	Value     float64    `json:"value"`
	Unit      string     `json:"unit"` // "%" or "X"
	Type      SignalType `json:"type"`
	MsgID     int64      `json:"msg_id"`
	RawText   string     `json:"raw_text"`
	Mint      string     `json:"mint,omitempty"` // Resolved mint address
	Timestamp int64      `json:"timestamp"`
	Reached2X bool       `json:"reached_2x"` // Did this token hit 2X?
	InitialMC float64    `json:"initial_mc"` // Parsed Market Cap from alert
}

// Parser handles signal parsing from Telegram messages
type Parser struct {
	// Pattern: 📈 TOKEN is up VALUE% 📈 or 📈 TOKEN is up VALUEX 📈
	pattern *regexp.Regexp
	// CA pattern: Base58 Solana address (43-44 chars)
	caPattern *regexp.Regexp
	// FIX: GeckoTerminal URL pattern for CA extraction
	geckoPattern *regexp.Regexp
	// FIX: Pump.fun URL pattern for CA extraction
	pumpPattern *regexp.Regexp
	// FIX: MC Pattern: 💰 MC: $35,427 or $31.1K
	mcPattern *regexp.Regexp
}

// NewParser creates a new signal parser
func NewParser() *Parser {
	return &Parser{
		// Match: 📈 TOKEN is up 50% 📈 or 📈 TOKEN is up 2.5X 📈
		pattern: regexp.MustCompile(`📈\s*([A-Z0-9]+)\s+is\s+up\s+([0-9.]+)\s*(%|X)\s*📈`),
		// Match Solana addresses (Base58, 43-44 chars)
		caPattern: regexp.MustCompile(`[1-9A-HJ-NP-Za-km-z]{43,44}`),
		// FIX: Match CA from GeckoTerminal URLs
		// Example: https://www.geckoterminal.com/solana/tokens/CKaTvCdrnARQAUK2ZmAXGroXqZ8BUNHESg1Zokngpump
		geckoPattern: regexp.MustCompile(`geckoterminal\.com/solana/tokens/([1-9A-HJ-NP-Za-km-z]{32,44})`),
		// FIX: Match CA from Pump.fun URLs
		// Example: https://pump.fun/CKaTvCdrnARQAUK2ZmAXGroXqZ8BUNHESg1Zokngpump
		pumpPattern: regexp.MustCompile(`pump\.fun/([1-9A-HJ-NP-Za-km-z]{32,44})`),
		// Match: 💰 MC: $35,427 or MC: $35.4K
		mcPattern: regexp.MustCompile(`MC:\s*\$([0-9,.]+[KMB]?)`),
	}
}

// Parse extracts signal data from message text
func (p *Parser) Parse(text string, msgID int64) (*Signal, error) {
	// Clean text of invisible characters (LTRM, ZWSP, etc)
	cleanText := strings.Map(func(r rune) rune {
		if r == '\u200e' || r == '\u200f' || r == '\u200b' {
			return -1
		}
		return r
	}, text)

	matches := p.pattern.FindStringSubmatch(cleanText)
	if len(matches) != 4 {
		return nil, nil // No match, not an error
	}

	value, err := strconv.ParseFloat(matches[2], 64)
	if err != nil {
		return nil, err
	}

	signal := &Signal{
		TokenName: strings.ToUpper(matches[1]),
		Value:     value,
		Unit:      matches[3],
		MsgID:     msgID,
		RawText:   text,
		Type:      SignalIgnore, // Default
	}

	// Try to extract CA from message
	signal.Mint = p.extractCA(text)

	// Try to extract MC
	signal.InitialMC = p.extractMC(text)

	return signal, nil
}

// extractMC extracts Market Cap value
func (p *Parser) extractMC(text string) float64 {
	matches := p.mcPattern.FindStringSubmatch(text)
	if len(matches) < 2 {
		return 0
	}

	valStr := strings.ToUpper(matches[1])
	multiplier := 1.0

	if strings.HasSuffix(valStr, "K") {
		multiplier = 1000.0
		valStr = strings.TrimSuffix(valStr, "K")
	} else if strings.HasSuffix(valStr, "M") {
		multiplier = 1000000.0
		valStr = strings.TrimSuffix(valStr, "M")
	} else if strings.HasSuffix(valStr, "B") {
		multiplier = 1000000000.0
		valStr = strings.TrimSuffix(valStr, "B")
	}

	// Remove commas
	valStr = strings.ReplaceAll(valStr, ",", "")

	val, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return 0
	}

	return val * multiplier
}

// Classify determines the signal type based on config thresholds
func (p *Parser) Classify(signal *Signal, minEntryPercent, takeProfitMultiple float64) {
	if signal == nil {
		return
	}

	switch signal.Unit {
	case "%":
		if signal.Value >= minEntryPercent {
			signal.Type = SignalEntry
		}
	case "X":
		if signal.Value >= takeProfitMultiple {
			signal.Type = SignalExit
		}
	}
}

// extractCA attempts to extract a Solana contract address from text
// FIX: Priority order for CA extraction:
// 1. GeckoTerminal URL (most reliable)
// 2. Pump.fun URL
// 3. Generic Base58 pattern (fallback)
func (p *Parser) extractCA(text string) string {
	// Priority 1: GeckoTerminal URL
	if matches := p.geckoPattern.FindStringSubmatch(text); len(matches) >= 2 {
		ca := matches[1]
		if len(ca) >= 32 && len(ca) <= 44 {
			return ca
		}
	}

	// Priority 2: Pump.fun URL
	if matches := p.pumpPattern.FindStringSubmatch(text); len(matches) >= 2 {
		ca := matches[1]
		if len(ca) >= 32 && len(ca) <= 44 {
			return ca
		}
	}

	// Priority 3: Generic Base58 pattern (fallback)
	matches := p.caPattern.FindAllString(text, -1)
	if len(matches) == 0 {
		return ""
	}
	// Return first valid CA found
	for _, m := range matches {
		// Basic validation: length check
		if len(m) >= 43 && len(m) <= 44 {
			return m
		}
	}
	return ""
}

// ParsedSignal is the incoming payload from Python listener
type ParsedSignal struct {
	Text      string `json:"text"`
	MsgID     int64  `json:"msg_id"`
	Timestamp int64  `json:"timestamp"`
}
