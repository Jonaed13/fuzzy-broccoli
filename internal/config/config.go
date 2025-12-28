package config

import (
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

// Config holds all bot configuration
type Config struct {
	Wallet     WalletConfig     `mapstructure:"wallet"`
	RPC        RPCConfig        `mapstructure:"rpc"`
	Trading    TradingConfig    `mapstructure:"trading"`
	Fees       FeesConfig       `mapstructure:"fees"`
	Jupiter    JupiterConfig    `mapstructure:"jupiter"`
	Telegram   TelegramConfig   `mapstructure:"telegram"`
	Blockchain BlockchainConfig `mapstructure:"blockchain"`
	Storage    StorageConfig    `mapstructure:"storage"`
	TUI        TUIConfig        `mapstructure:"tui"`
	WebSocket  WebSocketConfig  `mapstructure:"websocket"`
}

type WalletConfig struct {
	PrivateKeyEnv string `mapstructure:"private_key_env"`
	BaseMint      string `mapstructure:"base_mint"`
}

type RPCConfig struct {
	ShyftURL      string `mapstructure:"shyft_url"`
	ShyftAPIKeyEnv string `mapstructure:"shyft_api_key_env"`
	FallbackURL   string `mapstructure:"fallback_url"`
}

type TradingConfig struct {
	MinEntryPercent       float64 `mapstructure:"min_entry_percent"`
	TakeProfitMultiple    float64 `mapstructure:"take_profit_multiple"`
	StopLossPercent       float64 `mapstructure:"stop_loss_percent"`      // e.g., -50.0 = sell if PnL <= -50%
	MaxAllocPercent       float64 `mapstructure:"max_alloc_percent"`
	MaxOpenPositions      int     `mapstructure:"max_open_positions"`
	AutoTradingEnabled    bool    `mapstructure:"auto_trading_enabled"`
	
	// Partial Profit-Taking (sell X% at Y multiple)
	PartialProfitPercent  float64 `mapstructure:"partial_profit_percent"`  // e.g., 50 = sell 50%
	PartialProfitMultiple float64 `mapstructure:"partial_profit_multiple"` // e.g., 1.5 = at 1.5X
	
	// Time-Based Exit (auto-sell after X minutes)
	MaxHoldMinutes        int     `mapstructure:"max_hold_minutes"` // 0 = disabled

	// Simulation
	SimulationMode        bool    `mapstructure:"simulation_mode"`  // Enable for CLI test verification
}

type FeesConfig struct {
	StaticPriorityFeeSol float64 `mapstructure:"static_priority_fee_sol"`
	StaticGasFeeSol      float64 `mapstructure:"static_gas_fee_sol"`
}

type JupiterConfig struct {
	QuoteAPIURL    string `mapstructure:"quote_api_url"`
	SlippageBps    int    `mapstructure:"slippage_bps"`
	TimeoutSeconds int    `mapstructure:"timeout_seconds"`
}

type TelegramConfig struct {
	ListenPort int    `mapstructure:"listen_port"`
	ListenHost string `mapstructure:"listen_host"`
}

type BlockchainConfig struct {
	BlockhashRefreshMs    int `mapstructure:"blockhash_refresh_ms"`
	BlockhashTTLSeconds   int `mapstructure:"blockhash_ttl_seconds"`
	BalanceRefreshSeconds int `mapstructure:"balance_refresh_seconds"`
}

type StorageConfig struct {
	SQLitePath        string `mapstructure:"sqlite_path"`
	SignalsBufferSize int    `mapstructure:"signals_buffer_size"`
}

type TUIConfig struct {
	RefreshRateMs int `mapstructure:"refresh_rate_ms"`
	LogLines      int `mapstructure:"log_lines"`
}

type WebSocketConfig struct {
	ShyftURL        string `mapstructure:"shyft_url"`
	ReconnectDelayMs int   `mapstructure:"reconnect_delay_ms"`
	PingIntervalMs   int   `mapstructure:"ping_interval_ms"`
}

// Manager handles config loading and hot-reload
type Manager struct {
	mu       sync.RWMutex
	config   *Config
	viper    *viper.Viper
	onChange func(*Config)
}

// NewManager creates a new config manager
func NewManager(configPath string) (*Manager, error) {
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")

	// Set Defaults (Hardening)
	v.SetDefault("blockchain.blockhash_refresh_ms", 100)
	v.SetDefault("blockchain.blockhash_ttl_seconds", 60)
	v.SetDefault("blockchain.balance_refresh_seconds", 5)
	v.SetDefault("jupiter.quote_api_url", "https://quote-api.jup.ag/v6/quote")
	v.SetDefault("jupiter.slippage_bps", 500) // 5%
	v.SetDefault("jupiter.timeout_seconds", 10)
	v.SetDefault("rpc.shyft_api_key_env", "SHYFT_API_KEY")
	v.SetDefault("rpc.fallback_url", "https://api.mainnet-beta.solana.com")
	v.SetDefault("storage.sqlite_path", "./data/bot.db")
	v.SetDefault("storage.signals_buffer_size", 100)
	v.SetDefault("tui.refresh_rate_ms", 100)
	v.SetDefault("tui.log_lines", 100)
	v.SetDefault("wallet.private_key_env", "WALLET_PRIVATE_KEY")

	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	// Manual fallback if unmarshal leaves zero values (double check)
	if cfg.Jupiter.QuoteAPIURL == "" { cfg.Jupiter.QuoteAPIURL = "https://quote-api.jup.ag/v6/quote" }
	if cfg.Storage.SQLitePath == "" { cfg.Storage.SQLitePath = "./data/bot.db" }

	m := &Manager{
		config: &cfg,
		viper:  v,
	}

	// Watch for config changes
	v.WatchConfig()
	v.OnConfigChange(func(e fsnotify.Event) {
		log.Info().Str("file", e.Name).Msg("config file changed, reloading")
		m.reload()
	})

	return m, nil
}

// Get returns the current config (thread-safe)
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// GetTrading returns trading config (most frequently accessed)
func (m *Manager) GetTrading() TradingConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.Trading
}

// SetOnChange registers a callback for config changes
func (m *Manager) SetOnChange(fn func(*Config)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onChange = fn
}

// Update modifies config values and saves to file
func (m *Manager) Update(fn func(*Config)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Apply changes
	fn(m.config)

	// Update viper values
	m.viper.Set("trading.min_entry_percent", m.config.Trading.MinEntryPercent)
	m.viper.Set("trading.take_profit_multiple", m.config.Trading.TakeProfitMultiple)
	m.viper.Set("trading.max_alloc_percent", m.config.Trading.MaxAllocPercent)
	m.viper.Set("trading.max_open_positions", m.config.Trading.MaxOpenPositions)
	m.viper.Set("trading.stop_loss_percent", m.config.Trading.StopLossPercent)
	m.viper.Set("trading.auto_trading_enabled", m.config.Trading.AutoTradingEnabled)
	m.viper.Set("fees.static_priority_fee_sol", m.config.Fees.StaticPriorityFeeSol)

	// Write to file
	if err := m.viper.WriteConfig(); err != nil {
		return err
	}

	if m.onChange != nil {
		m.onChange(m.config)
	}

	return nil
}

func (m *Manager) reload() {
	m.mu.Lock()
	defer m.mu.Unlock()

	var cfg Config
	if err := m.viper.Unmarshal(&cfg); err != nil {
		log.Error().Err(err).Msg("failed to unmarshal config on reload")
		return
	}

	m.config = &cfg
	if m.onChange != nil {
		m.onChange(&cfg)
	}
}

// GetPrivateKey loads private key from environment
func (m *Manager) GetPrivateKey() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return os.Getenv(m.config.Wallet.PrivateKeyEnv)
}

// GetShyftAPIKey loads Shyft API key from environment
func (m *Manager) GetShyftAPIKey() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return os.Getenv(m.config.RPC.ShyftAPIKeyEnv)
}

// GetBlockhashRefresh returns blockhash refresh interval as duration
func (m *Manager) GetBlockhashRefresh() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return time.Duration(m.config.Blockchain.BlockhashRefreshMs) * time.Millisecond
}

// GetBalanceRefresh returns balance refresh interval as duration
func (m *Manager) GetBalanceRefresh() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return time.Duration(m.config.Blockchain.BalanceRefreshSeconds) * time.Second
}
