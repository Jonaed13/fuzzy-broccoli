package blockchain

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// CachedBlockhash holds the cached blockhash with metadata
type CachedBlockhash struct {
	Hash                 string
	LastValidBlockHeight uint64
	FetchedAt            time.Time
}

// BlockhashCache provides a double-buffered blockhash cache with aggressive prefetching
type BlockhashCache struct {
	// Double buffer: current is always valid, next is being fetched
	current atomic.Pointer[CachedBlockhash]
	next    atomic.Pointer[CachedBlockhash]

	rpc      *RPCClient
	ttl      time.Duration
	interval time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup

	// Metrics
	hits   atomic.Int64
	misses atomic.Int64
}

// NewBlockhashCache creates a new double-buffered blockhash cache
// refreshInterval should be 100ms for aggressive prefetching
func NewBlockhashCache(rpc *RPCClient, refreshInterval, ttl time.Duration) *BlockhashCache {
	return &BlockhashCache{
		rpc:      rpc,
		interval: refreshInterval,
		ttl:      ttl,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the background refresh goroutine
func (c *BlockhashCache) Start() error {
	// Initial fetch - must succeed
	if err := c.fetchAndRotate(); err != nil {
		return err
	}

	c.wg.Add(1)
	go c.prefetchLoop()

	log.Info().
		Dur("interval", c.interval).
		Dur("ttl", c.ttl).
		Msg("blockhash cache started (double-buffer mode)")

	return nil
}

// Stop stops the background refresh
func (c *BlockhashCache) Stop() {
	close(c.stopCh)
	c.wg.Wait()
}

// Get returns the cached blockhash - NEVER BLOCKS
// This is the hot path, must be as fast as possible
func (c *BlockhashCache) Get() (string, error) {
	cached := c.current.Load()

	// Fast path: cached is valid
	if cached != nil && time.Since(cached.FetchedAt) < c.ttl {
		c.hits.Add(1)
		return cached.Hash, nil
	}

	// Try next buffer
	next := c.next.Load()
	if next != nil && time.Since(next.FetchedAt) < c.ttl {
		c.hits.Add(1)
		return next.Hash, nil
	}

	// Both buffers stale - force synchronous refresh (rare)
	c.misses.Add(1)
	log.Warn().Msg("blockhash cache miss, forcing sync refresh")
	
	if err := c.fetchAndRotate(); err != nil {
		return "", err
	}

	return c.current.Load().Hash, nil
}

// GetWithHeight returns blockhash and last valid block height
func (c *BlockhashCache) GetWithHeight() (string, uint64, error) {
	cached := c.current.Load()

	if cached != nil && time.Since(cached.FetchedAt) < c.ttl {
		return cached.Hash, cached.LastValidBlockHeight, nil
	}

	next := c.next.Load()
	if next != nil && time.Since(next.FetchedAt) < c.ttl {
		return next.Hash, next.LastValidBlockHeight, nil
	}

	if err := c.fetchAndRotate(); err != nil {
		return "", 0, err
	}

	cached = c.current.Load()
	return cached.Hash, cached.LastValidBlockHeight, nil
}

// Age returns time since last successful fetch
func (c *BlockhashCache) Age() time.Duration {
	cached := c.current.Load()
	if cached == nil {
		return 0
	}
	return time.Since(cached.FetchedAt)
}

// HitRate returns the cache hit rate percentage
func (c *BlockhashCache) HitRate() float64 {
	hits := c.hits.Load()
	misses := c.misses.Load()
	total := hits + misses
	if total == 0 {
		return 100.0
	}
	return float64(hits) / float64(total) * 100
}

func (c *BlockhashCache) prefetchLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			if err := c.fetchAndRotate(); err != nil {
				log.Warn().Err(err).Msg("blockhash prefetch failed")
			}
		}
	}
}

func (c *BlockhashCache) fetchAndRotate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := c.rpc.GetLatestBlockhash(ctx)
	if err != nil {
		return err
	}

	newHash := &CachedBlockhash{
		Hash:                 result.Value.Blockhash,
		LastValidBlockHeight: result.Value.LastValidBlockHeight,
		FetchedAt:            time.Now(),
	}

	// Rotate: current -> (discard), next -> current, new -> next
	current := c.current.Load()
	c.current.Store(c.next.Load())
	c.next.Store(newHash)

	// Bootstrap case: if current was nil, set it directly
	if current == nil {
		c.current.Store(newHash)
	}

	log.Debug().
		Str("hash", result.Value.Blockhash[:16]+"...").
		Uint64("height", result.Value.LastValidBlockHeight).
		Float64("hitRate", c.HitRate()).
		Msg("blockhash prefetched")

	return nil
}
