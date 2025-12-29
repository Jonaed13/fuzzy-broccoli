package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"

	"github.com/mr-tron/base58"
)

func main() {
	// Generate new Ed25519 keypair
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}

	// Encode private key (64 bytes) as Base58
	privateKeyBase58 := base58.Encode(privateKey)

	// Encode public key as Base58 (this is the wallet address)
	address := base58.Encode(publicKey)

	fmt.Println("=== NEW SOLANA TEST WALLET ===")
	fmt.Println("")
	fmt.Println("Address (Public Key):")
	fmt.Println(address)
	fmt.Println("")
	fmt.Println("Private Key (Base58):")
	fmt.Println(privateKeyBase58)
	fmt.Println("")
	fmt.Println("Add to .env:")
	fmt.Printf("WALLET_PRIVATE_KEY=%s\n", privateKeyBase58)
}
