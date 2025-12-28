package blockchain

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"sync"

	"github.com/mr-tron/base58"
	"github.com/rs/zerolog/log"
)

// Wallet holds the keypair for signing transactions
type Wallet struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	address    string
}

// NewWallet creates a wallet from a base58-encoded private key.
//
// SECURITY WARNING: This function accepts a private key as a plain string,
// which is a significant security risk. Storing private keys in configuration
// files or source code can lead to theft of funds.
//
// RECOMMENDED PRACTICE: Load the private key from a secure source at runtime,
// such as an environment variable or a dedicated secret management service
// (e.g., HashiCorp Vault, AWS Secrets Manager).
func NewWallet(privateKeyBase58 string) (*Wallet, error) {
	// Decode base58 private key
	privateKeyBytes, err := base58.Decode(privateKeyBase58)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}

	// Private key should be 64 bytes (32 seed + 32 public key)
	// or 32 bytes (seed only)
	var privateKey ed25519.PrivateKey

	switch len(privateKeyBytes) {
	case 64:
		privateKey = ed25519.PrivateKey(privateKeyBytes)
	case 32:
		privateKey = ed25519.NewKeyFromSeed(privateKeyBytes)
	default:
		return nil, fmt.Errorf("invalid private key length: %d (expected 32 or 64)", len(privateKeyBytes))
	}

	publicKey := privateKey.Public().(ed25519.PublicKey)
	address := base58.Encode(publicKey)

	log.Info().Str("address", address).Msg("wallet loaded")

	return &Wallet{
		privateKey: privateKey,
		publicKey:  publicKey,
		address:    address,
	}, nil
}

// Address returns the wallet's public key as Base58 string
func (w *Wallet) Address() string {
	return w.address
}

// PublicKey returns the wallet's public key bytes
func (w *Wallet) PublicKey() []byte {
	return w.publicKey
}

// Sign signs a message with the wallet's private key
func (w *Wallet) Sign(message []byte) []byte {
	return ed25519.Sign(w.privateKey, message)
}

// BalanceTracker maintains the wallet's SOL balance
type BalanceTracker struct {
	mu              sync.RWMutex
	wallet          *Wallet
	rpc             *RPCClient
	balanceLamports uint64
}

// NewBalanceTracker creates a new balance tracker
func NewBalanceTracker(wallet *Wallet, rpc *RPCClient) *BalanceTracker {
	return &BalanceTracker{
		wallet: wallet,
		rpc:    rpc,
	}
}

// Refresh updates the balance from RPC
func (b *BalanceTracker) Refresh(ctx context.Context) error {
	balance, err := b.rpc.GetBalance(ctx, b.wallet.Address())
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.balanceLamports = balance
	b.mu.Unlock()
	return nil
}

// BalanceLamports returns balance in lamports
func (b *BalanceTracker) BalanceLamports() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.balanceLamports
}

// BalanceSOL returns balance in SOL
func (b *BalanceTracker) BalanceSOL() float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return float64(b.balanceLamports) / 1e9
}

// SetBalance directly sets balance (for WebSocket updates)
func (b *BalanceTracker) SetBalance(lamports uint64) {
	b.mu.Lock()
	b.balanceLamports = lamports
	b.mu.Unlock()
}

// HasSufficientBalance checks if wallet can afford a trade
func (b *BalanceTracker) HasSufficientBalance(amountLamports, feesLamports uint64) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.balanceLamports >= amountLamports+feesLamports
}

// SignTransaction signs a serialized transaction
func (w *Wallet) SignTransaction(serializedTx []byte) (string, error) {
	// For Solana, the signature is prepended to the transaction
	// The message to sign is the transaction without signatures

	// Sign the transaction message
	signature := w.Sign(serializedTx)

	// Combine signature + serialized transaction
	// Note: This is simplified - actual Solana transaction format is more complex
	signed := append(signature, serializedTx...)

	return base64.StdEncoding.EncodeToString(signed), nil
}
