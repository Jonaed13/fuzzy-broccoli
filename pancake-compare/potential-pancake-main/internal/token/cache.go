package token

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

// Cache provides thread-safe token mint lookup
type Cache struct {
	mu     sync.RWMutex
	tokens map[string]string // TokenName -> Mint
	path   string
}

// NewCache creates a new token cache from JSON file
func NewCache(path string) (*Cache, error) {
	c := &Cache{
		tokens: make(map[string]string),
		path:   path,
	}

	if err := c.load(); err != nil {
		return nil, err
	}

	// Watch for file changes
	go c.watchFile()

	return c, nil
}

// Get returns the mint address for a token name
func (c *Cache) Get(tokenName string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	mint, ok := c.tokens[tokenName]
	return mint, ok
}

// Set adds or updates a token in the cache
func (c *Cache) Set(tokenName, mint string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokens[tokenName] = mint
}

// Size returns the number of tokens in cache
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.tokens)
}

// Save writes the cache to file
func (c *Cache) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, err := json.MarshalIndent(c.tokens, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(c.path, data, 0644)
}

func (c *Cache) load() error {
	data, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn().Str("path", c.path).Msg("token cache file not found, starting empty")
			return nil
		}
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := json.Unmarshal(data, &c.tokens); err != nil {
		return err
	}

	log.Info().Int("count", len(c.tokens)).Msg("token cache loaded")
	return nil
}

func (c *Cache) watchFile() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Error().Err(err).Msg("failed to create file watcher for token cache")
		return
	}
	defer watcher.Close()

	if err := watcher.Add(c.path); err != nil {
		log.Error().Err(err).Msg("failed to watch token cache file")
		return
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				log.Info().Msg("token cache file changed, reloading")
				if err := c.load(); err != nil {
					log.Error().Err(err).Msg("failed to reload token cache")
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Error().Err(err).Msg("token cache watcher error")
		}
	}
}
