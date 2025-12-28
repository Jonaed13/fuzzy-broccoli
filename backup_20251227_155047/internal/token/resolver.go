package token

import (
	"github.com/rs/zerolog/log"
)

// FIX: O(1) Base58 lookup table instead of O(n*58) nested loops
var base58Set = func() [256]bool {
	var set [256]bool
	const base58Chars = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	for i := 0; i < len(base58Chars); i++ {
		set[base58Chars[i]] = true
	}
	return set
}()

// Resolver handles token name to mint address resolution
type Resolver struct {
	cache *Cache
}

// NewResolver creates a new token resolver
func NewResolver(cache *Cache) *Resolver {
	return &Resolver{
		cache: cache,
	}
}

// Resolve returns the mint address for a token name
// Priority:
// 1. CA already provided (passthrough)
// 2. Cache lookup
// 3. (Future) RPC lookup
func (r *Resolver) Resolve(tokenNameOrCA string) string {
	// Check if it's already a CA (Base58, 43-44 chars)
	if len(tokenNameOrCA) >= 43 && len(tokenNameOrCA) <= 44 {
		if isValidBase58(tokenNameOrCA) {
			log.Debug().Str("ca", tokenNameOrCA).Msg("token already a CA")
			return tokenNameOrCA
		}
	}

	// Cache lookup
	if mint, ok := r.cache.Get(tokenNameOrCA); ok {
		log.Debug().
			Str("token", tokenNameOrCA).
			Str("mint", mint).
			Msg("token resolved from cache")
		return mint
	}

	log.Warn().
		Str("token", tokenNameOrCA).
		Msg("token not found in cache")
	return ""
}

// AddToken adds a new token to the cache and saves
func (r *Resolver) AddToken(name, mint string) error {
	r.cache.Set(name, mint)
	return r.cache.Save()
}

// CacheSize returns number of cached tokens
func (r *Resolver) CacheSize() int {
	return r.cache.Size()
}

// isValidBase58 checks if string contains only valid Base58 characters
// FIX: O(1) per character using lookup table instead of O(58) nested loop
func isValidBase58(s string) bool {
	for i := 0; i < len(s); i++ {
		if !base58Set[s[i]] {
			return false
		}
	}
	return true
}
