package main

import (
	"fmt"
	"solana-pump-bot/internal/signal"
)

func main() {
	p := signal.NewParser()
	texts := []string{
		"📈 LEGEND is up 51.0% 📈",
		"📈 LEGEND is up 2.0X 📈",
		"📈 LEGEND is up 3.0X 📈",
	}

	for _, t := range texts {
		s, _ := p.Parse(t, 1)
		p.Classify(s, 50, 2.0)
		fmt.Printf("Text: %s | Unit: %s | Value: %.2f | Type: %s\n", t, s.Unit, s.Value, s.Type)
	}
}
