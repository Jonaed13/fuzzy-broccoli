package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// Solana JSON-RPC WebSocket request/response types
type WSRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      uint64        `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params,omitempty"`
}

type WSResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *WSError        `json:"error,omitempty"`
	Params  *WSParams       `json:"params,omitempty"`
}

type WSError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type WSParams struct {
	Result       json.RawMessage `json:"result"`
	Subscription uint64          `json:"subscription"`
}

// SubscriptionHandler is called when a subscription receives data
type SubscriptionHandler func(data json.RawMessage)

// SubscriptionInfo stores subscription details for restoration after reconnect
type SubscriptionInfo struct {
	Method  string
	Params  []interface{}
	Handler SubscriptionHandler
}

// Client manages WebSocket connection to Solana RPC
type Client struct {
	url    string
	conn   *websocket.Conn
	connMu sync.RWMutex

	// Request ID counter
	requestID atomic.Uint64

	// Pending requests (waiting for response)
	pending   map[uint64]chan WSResponse
	pendingMu sync.RWMutex

	// Active subscriptions
	subscriptions map[uint64]SubscriptionHandler
	subMu         sync.RWMutex

	// FIX: Store subscription info for restoration after reconnect
	subInfo   map[uint64]SubscriptionInfo
	subInfoMu sync.RWMutex

	// Reconnect settings
	reconnectDelay time.Duration
	pingInterval   time.Duration

	// Control
	ctx       context.Context
	cancel    context.CancelFunc
	connected atomic.Bool

	// FIX: Goroutine control to prevent leaks
	loopCtx      context.Context
	loopCancel   context.CancelFunc
	reconnecting atomic.Bool

	// Callbacks
	onConnect    func()
	onDisconnect func(error)
}

// NewClient creates a new WebSocket client
func NewClient(url string, reconnectDelay, pingInterval time.Duration) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		url:            url,
		pending:        make(map[uint64]chan WSResponse),
		subscriptions:  make(map[uint64]SubscriptionHandler),
		subInfo:        make(map[uint64]SubscriptionInfo), // FIX: Initialize subscription info map
		reconnectDelay: reconnectDelay,
		pingInterval:   pingInterval,
		ctx:            ctx,
		cancel:         cancel,
	}
}

// SetCallbacks sets connection callbacks
func (c *Client) SetCallbacks(onConnect func(), onDisconnect func(error)) {
	c.onConnect = onConnect
	c.onDisconnect = onDisconnect
}

// Connect establishes WebSocket connection
func (c *Client) Connect() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	// FIX: Cancel any existing read/ping loops before starting new ones
	if c.loopCancel != nil {
		c.loopCancel()
	}

	// Close existing connection cleanly
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(c.ctx, c.url, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	c.conn = conn
	c.connected.Store(true)

	// FIX: Create new context for this connection's goroutines
	c.loopCtx, c.loopCancel = context.WithCancel(c.ctx)

	urlDisplay := c.url
	if len(urlDisplay) > 40 {
		urlDisplay = urlDisplay[:40] + "..."
	}
	log.Info().Str("url", urlDisplay).Msg("WebSocket connected")

	// Start read loop with new context
	go c.readLoop(c.loopCtx)

	// Start ping loop with new context
	go c.pingLoop(c.loopCtx)

	if c.onConnect != nil {
		go c.onConnect()
	}

	return nil
}

// Close disconnects the WebSocket
func (c *Client) Close() {
	c.cancel()

	// FIX: Cancel loop goroutines
	if c.loopCancel != nil {
		c.loopCancel()
	}

	c.connMu.Lock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.connMu.Unlock()

	c.connected.Store(false)
}

// IsConnected returns connection status
func (c *Client) IsConnected() bool {
	return c.connected.Load()
}

// Subscribe creates a new subscription
func (c *Client) Subscribe(method string, params []interface{}, handler SubscriptionHandler) (uint64, error) {
	resp, err := c.call(method, params)
	if err != nil {
		return 0, err
	}

	var subID uint64
	if err := json.Unmarshal(resp.Result, &subID); err != nil {
		return 0, fmt.Errorf("parse subscription ID: %w", err)
	}

	c.subMu.Lock()
	c.subscriptions[subID] = handler
	c.subMu.Unlock()

	// FIX: Store subscription info for restoration after reconnect
	c.subInfoMu.Lock()
	c.subInfo[subID] = SubscriptionInfo{
		Method:  method,
		Params:  params,
		Handler: handler,
	}
	c.subInfoMu.Unlock()

	log.Debug().Uint64("subID", subID).Str("method", method).Msg("subscription created")

	return subID, nil
}

// Unsubscribe removes a subscription
func (c *Client) Unsubscribe(method string, subID uint64) error {
	c.subMu.Lock()
	delete(c.subscriptions, subID)
	c.subMu.Unlock()

	// FIX: Remove subscription info
	c.subInfoMu.Lock()
	delete(c.subInfo, subID)
	c.subInfoMu.Unlock()

	_, err := c.call(method, []interface{}{subID})
	return err
}

// AccountSubscribe subscribes to account changes
func (c *Client) AccountSubscribe(pubkey string, handler SubscriptionHandler) (uint64, error) {
	params := []interface{}{
		pubkey,
		map[string]interface{}{
			"encoding":   "jsonParsed",
			"commitment": "confirmed",
		},
	}
	return c.Subscribe("accountSubscribe", params, handler)
}

// SignatureSubscribe subscribes to transaction signature status
func (c *Client) SignatureSubscribe(signature string, handler SubscriptionHandler) (uint64, error) {
	params := []interface{}{
		signature,
		map[string]interface{}{
			"commitment": "confirmed",
		},
	}
	return c.Subscribe("signatureSubscribe", params, handler)
}

// call sends a request and waits for response
func (c *Client) call(method string, params []interface{}) (*WSResponse, error) {
	id := c.requestID.Add(1)

	req := WSRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	// Create response channel
	respCh := make(chan WSResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	// Send request
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	if err := conn.WriteJSON(req); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	// Wait for response with timeout
	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return &resp, nil
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("timeout")
	case <-c.ctx.Done():
		return nil, c.ctx.Err()
	}
}

// readLoop reads messages from WebSocket
// FIX: Accept context parameter to allow clean goroutine termination
func (c *Client) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.connMu.RLock()
		conn := c.conn
		c.connMu.RUnlock()

		if conn == nil {
			return
		}

		var resp WSResponse
		if err := conn.ReadJSON(&resp); err != nil {
			// Check if we're shutting down
			select {
			case <-ctx.Done():
				return
			default:
			}

			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Info().Msg("WebSocket closed normally")
			} else {
				log.Error().Err(err).Msg("WebSocket read error")
			}
			c.handleDisconnect(err)
			return
		}

		// Handle response to pending request
		if resp.ID != 0 {
			c.pendingMu.RLock()
			ch, ok := c.pending[resp.ID]
			c.pendingMu.RUnlock()
			if ok {
				ch <- resp
			}
			continue
		}

		// Handle subscription notification
		if resp.Method != "" && resp.Params != nil {
			c.subMu.RLock()
			handler, ok := c.subscriptions[resp.Params.Subscription]
			c.subMu.RUnlock()
			if ok {
				go handler(resp.Params.Result)
			}
		}
	}
}

// pingLoop sends periodic pings
// FIX: Accept context parameter to allow clean goroutine termination
func (c *Client) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(c.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.connMu.RLock()
			conn := c.conn
			c.connMu.RUnlock()

			if conn != nil {
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					log.Warn().Err(err).Msg("ping failed")
				}
			}
		}
	}
}

// handleDisconnect handles connection loss
func (c *Client) handleDisconnect(err error) {
	c.connected.Store(false)

	if c.onDisconnect != nil {
		go c.onDisconnect(err)
	}

	// FIX: Prevent multiple reconnect loops
	if c.reconnecting.Swap(true) {
		return // Already reconnecting
	}

	// Auto-reconnect
	go c.reconnectLoop()
}

// reconnectLoop attempts to reconnect
func (c *Client) reconnectLoop() {
	defer c.reconnecting.Store(false)

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(c.reconnectDelay):
			log.Info().Msg("attempting WebSocket reconnect...")
			if err := c.Connect(); err != nil {
				log.Error().Err(err).Msg("reconnect failed")
				continue
			}

			// FIX: Re-subscribe existing subscriptions using stored info
			c.resubscribe()
			return
		}
	}
}

// resubscribe re-creates subscriptions after reconnect
// FIX: Actually restore subscriptions instead of just logging
func (c *Client) resubscribe() {
	c.subInfoMu.RLock()
	defer c.subInfoMu.RUnlock()

	if len(c.subInfo) == 0 {
		log.Debug().Msg("no subscriptions to restore")
		return
	}

	log.Info().Int("count", len(c.subInfo)).Msg("restoring subscriptions after reconnect")

	// Collect subscription info (we'll re-subscribe which will update the maps)
	infos := make([]SubscriptionInfo, 0, len(c.subInfo))
	for _, info := range c.subInfo {
		infos = append(infos, info)
	}

	// Clear old subscription IDs (they're invalid after reconnect)
	c.subMu.Lock()
	c.subscriptions = make(map[uint64]SubscriptionHandler)
	c.subMu.Unlock()

	// Must release read lock before calling Subscribe (which needs write lock)
	c.subInfoMu.RUnlock()
	c.subInfoMu.Lock()
	c.subInfo = make(map[uint64]SubscriptionInfo)
	c.subInfoMu.Unlock()
	c.subInfoMu.RLock() // Re-acquire for defer

	// Re-subscribe each
	restored := 0
	for _, info := range infos {
		_, err := c.Subscribe(info.Method, info.Params, info.Handler)
		if err != nil {
			log.Warn().Err(err).Str("method", info.Method).Msg("failed to restore subscription")
		} else {
			restored++
		}
	}

	log.Info().Int("restored", restored).Int("total", len(infos)).Msg("subscriptions restored")
}
