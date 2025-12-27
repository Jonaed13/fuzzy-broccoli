package health

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// Status represents the health status of a component
type Status struct {
	Name    string
	Healthy bool
	Latency time.Duration
	Error   string
}

// Checker periodically checks health of system components
type Checker struct {
	mu       sync.RWMutex
	statuses []Status
	rpcURL   string
	httpURL  string // Telegram listener endpoint
}

// NewChecker creates a new health checker
func NewChecker(rpcURL, httpURL string) *Checker {
	return &Checker{
		rpcURL:  rpcURL,
		httpURL: httpURL,
	}
}

// Start begins periodic health checks
func (c *Checker) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.check()
			}
		}
	}()
	
	// Initial check
	c.check()
}

func (c *Checker) check() {
	var statuses []Status
	
	// Check RPC
	rpcStatus := c.checkRPC()
	statuses = append(statuses, rpcStatus)
	
	// Check Telegram HTTP
	telegramStatus := c.checkHTTP()
	statuses = append(statuses, telegramStatus)
	
	c.mu.Lock()
	c.statuses = statuses
	c.mu.Unlock()
}

func (c *Checker) checkRPC() Status {
	start := time.Now()
	
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("POST", c.rpcURL, nil)
	req.Header.Set("Content-Type", "application/json")
	
	_, err := client.Do(req)
	latency := time.Since(start)
	
	status := Status{
		Name:    "RPC",
		Latency: latency,
		Healthy: err == nil,
	}
	if err != nil {
		status.Error = err.Error()
	}
	return status
}

func (c *Checker) checkHTTP() Status {
	start := time.Now()
	
	client := &http.Client{Timeout: 5 * time.Second}
	_, err := client.Get(c.httpURL + "/health")
	latency := time.Since(start)
	
	status := Status{
		Name:    "Telegram",
		Latency: latency,
		Healthy: err == nil,
	}
	if err != nil {
		status.Error = err.Error()
	}
	return status
}

// GetStatuses returns current health statuses
func (c *Checker) GetStatuses() []Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.statuses
}
