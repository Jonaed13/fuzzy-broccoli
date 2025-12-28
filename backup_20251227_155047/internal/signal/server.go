package signal

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"
)

// Handler processes incoming signals from Telegram listener
type Handler struct {
	parser      *Parser
	signalChan  chan *Signal
	minEntry    func() float64
	takeProfit  func() float64
	resolveMint func(string) string
}

// NewHandler creates a signal handler
func NewHandler(
	signalChan chan *Signal,
	minEntry func() float64,
	takeProfit func() float64,
	resolveMint func(string) string,
) *Handler {
	return &Handler{
		parser:      NewParser(),
		signalChan:  signalChan,
		minEntry:    minEntry,
		takeProfit:  takeProfit,
		resolveMint: resolveMint,
	}
}

// Server runs the HTTP server for receiving signals
type Server struct {
	app     *fiber.App
	handler *Handler
	host    string
	port    int
}

// NewServer creates a new signal server
func NewServer(host string, port int, handler *Handler) *Server {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ReadTimeout:           5 * time.Second,
		WriteTimeout:          5 * time.Second,
	})

	s := &Server{
		app:     app,
		handler: handler,
		host:    host,
		port:    port,
	}

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	// Health check
	s.app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status": "ok",
			"time":   time.Now().Unix(),
		})
	})

	// Signal endpoint
	s.app.Post("/signal", s.handleSignal)
}

func (s *Server) handleSignal(c *fiber.Ctx) error {
	var payload ParsedSignal
	if err := c.BodyParser(&payload); err != nil {
		log.Error().Err(err).Msg("failed to parse signal payload")
		return c.Status(400).JSON(fiber.Map{"error": "invalid payload"})
	}

	// Parse signal
	signal, err := s.handler.parser.Parse(payload.Text, payload.MsgID)
	if err != nil {
		log.Error().Err(err).Str("text", payload.Text).Msg("failed to parse signal")
		return c.Status(400).JSON(fiber.Map{"error": "parse error"})
	}

	if signal == nil {
		log.Debug().Str("text", payload.Text).Msg("no signal pattern matched")
		return c.JSON(fiber.Map{"status": "ignored", "reason": "no pattern match"})
	}

	signal.Timestamp = payload.Timestamp
	if signal.Timestamp == 0 {
		signal.Timestamp = time.Now().Unix()
	}

	// Classify signal
	s.handler.parser.Classify(signal, s.handler.minEntry(), s.handler.takeProfit())

	// Resolve mint if not already present
	if signal.Mint == "" && s.handler.resolveMint != nil {
		signal.Mint = s.handler.resolveMint(signal.TokenName)
	}

	log.Info().
		Str("token", signal.TokenName).
		Float64("value", signal.Value).
		Str("unit", signal.Unit).
		Str("type", string(signal.Type)).
		Str("mint", signal.Mint).
		Msg("signal received")

	// Send to channel (non-blocking)
	select {
	case s.handler.signalChan <- signal:
	default:
		log.Warn().Msg("signal channel full, dropping signal")
	}

	return c.JSON(fiber.Map{
		"status": "received",
		"signal": signal,
	})
}

// Start starts the HTTP server
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	log.Info().Str("addr", addr).Msg("starting signal server")
	return s.app.Listen(addr)
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown() error {
	return s.app.Shutdown()
}
