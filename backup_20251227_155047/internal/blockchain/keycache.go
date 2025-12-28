package blockchain

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mr-tron/base58"
	"github.com/rs/zerolog/log"
)

// CachedKeyManager handles auto-generated wallet keys with caching
type CachedKeyManager struct {
	keyPath      string
	refreshEvery time.Duration
	
	mu         sync.RWMutex
	privateKey []byte
	publicKey  ed25519.PublicKey
	address    string
	lastRefresh time.Time
}

// CachedKeyData is the JSON structure for cached key
type CachedKeyData struct {
	PrivateKey  string    `json:"private_key"`
	PublicKey   string    `json:"public_key"`
	Address     string    `json:"address"`
	GeneratedAt time.Time `json:"generated_at"`
}

// NewCachedKeyManager creates a key manager with auto-refresh
func NewCachedKeyManager(cacheDir string, refreshEvery time.Duration) *CachedKeyManager {
	return &CachedKeyManager{
		keyPath:      filepath.Join(cacheDir, "wallet_cache.json"),
		refreshEvery: refreshEvery,
	}
}

// GetOrGenerate returns cached key or generates new one
func (m *CachedKeyManager) GetOrGenerate() (*Wallet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Try to load from cache
	if m.loadFromCache() {
		log.Info().
			Str("address", m.address).
			Time("generatedAt", m.lastRefresh).
			Msg("loaded wallet from cache")
		
		return m.createWallet()
	}

	// Generate new key
	if err := m.generateNewKey(); err != nil {
		return nil, err
	}

	// Save to cache
	if err := m.saveToCache(); err != nil {
		log.Warn().Err(err).Msg("failed to cache wallet key")
	}

	log.Info().
		Str("address", m.address).
		Msg("generated new wallet (cached for 10 minutes)")
	
	return m.createWallet()
}

// GetAddress returns the current wallet address
func (m *CachedKeyManager) GetAddress() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.address
}

// ShouldRefresh checks if key should be refreshed
func (m *CachedKeyManager) ShouldRefresh() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return time.Since(m.lastRefresh) > m.refreshEvery
}

// Refresh generates a new key
func (m *CachedKeyManager) Refresh() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.generateNewKey(); err != nil {
		return err
	}

	if err := m.saveToCache(); err != nil {
		return err
	}

	log.Info().
		Str("address", m.address).
		Msg("wallet key refreshed")
	
	return nil
}

func (m *CachedKeyManager) loadFromCache() bool {
	data, err := os.ReadFile(m.keyPath)
	if err != nil {
		return false
	}

	var cached CachedKeyData
	if err := json.Unmarshal(data, &cached); err != nil {
		return false
	}

	// Check if expired
	if time.Since(cached.GeneratedAt) > m.refreshEvery {
		return false
	}

	m.privateKey, _ = base58.Decode(cached.PrivateKey)
	m.address = cached.Address
	m.lastRefresh = cached.GeneratedAt

	if len(m.privateKey) >= 64 {
		m.publicKey = ed25519.PublicKey(m.privateKey[32:64])
	}

	return true
}

func (m *CachedKeyManager) saveToCache() error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(m.keyPath), 0700); err != nil {
		return err
	}

	cached := CachedKeyData{
		PrivateKey:  base58.Encode(m.privateKey),
		Address:     m.address,
		GeneratedAt: m.lastRefresh,
	}

	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.keyPath, data, 0600)
}

func (m *CachedKeyManager) generateNewKey() error {
	// Generate Ed25519 keypair
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}

	m.publicKey = pub
	m.privateKey = priv
	m.address = base58.Encode(pub)
	m.lastRefresh = time.Now()

	return nil
}

func (m *CachedKeyManager) createWallet() (*Wallet, error) {
	return &Wallet{
		privateKey: m.privateKey,
		publicKey:  m.publicKey,
		address:    m.address,
	}, nil
}
