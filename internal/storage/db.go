package storage

import (
	"database/sql"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	_ "modernc.org/sqlite"
)

// DB wraps SQLite database
type DB struct {
	db *sql.DB
}

// Position represents an open position
type Position struct {
	Mint       string
	TokenName  string
	Size       float64
	EntryValue float64
	EntryUnit  string
	EntryTime  int64
	EntryTxSig string
	MsgID      int64
	Reached2X  bool // Track if we already counted 2X win
}

// Trade represents a completed trade
type Trade struct {
	ID         int64
	Mint       string
	TokenName  string
	Side       string  // "BUY" or "SELL"
	AmountSol  float64 // SOL spent/received
	EntryValue float64
	ExitValue  float64
	PnL        float64
	Duration   int64
	EntryTxSig string
	ExitTxSig  string
	Timestamp  int64
}

// Signal represents a logged signal
type Signal struct {
	ID         int64
	TokenName  string
	Value      float64
	Unit       string
	SignalType string
	MsgID      int64
	Timestamp  int64
	Mint       string
}

// NewDB creates a new database connection
func NewDB(path string) (*DB, error) {
	// Add connection options to path if not present
	// _pragma=journal_mode(WAL) & _pragma=synchronous(NORMAL)
	dsn := path
	if !strings.Contains(path, "?") {
		dsn += "?"
	} else {
		dsn += "&"
	}
	dsn += "_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	// Create tables
	if err := createTables(db); err != nil {
		return nil, err
	}

	log.Info().Str("path", path).Msg("database initialized")
	return &DB{db: db}, nil
}

func createTables(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS positions (
		mint TEXT PRIMARY KEY,
		token_name TEXT NOT NULL,
		size REAL NOT NULL,
		entry_value REAL NOT NULL,
		entry_unit TEXT NOT NULL,
		entry_time INTEGER NOT NULL,
		entry_tx_sig TEXT NOT NULL,
		msg_id INTEGER
	);

	CREATE TABLE IF NOT EXISTS trades (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		mint TEXT NOT NULL,
		token_name TEXT NOT NULL,
		side TEXT NOT NULL DEFAULT 'SELL',
		amount_sol REAL NOT NULL DEFAULT 0,
		entry_value REAL NOT NULL,
		exit_value REAL NOT NULL,
		pnl REAL NOT NULL,
		duration INTEGER NOT NULL,
		entry_tx_sig TEXT NOT NULL,
		exit_tx_sig TEXT NOT NULL,
		timestamp INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS signals (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token_name TEXT NOT NULL,
		value REAL NOT NULL,
		unit TEXT NOT NULL,
		signal_type TEXT NOT NULL,
		msg_id INTEGER NOT NULL,
		timestamp INTEGER NOT NULL
	);

	-- Phase 8: Bot stats persistence
	CREATE TABLE IF NOT EXISTS bot_stats (
		key TEXT PRIMARY KEY,
		value INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_trades_timestamp ON trades(timestamp);
	CREATE INDEX IF NOT EXISTS idx_signals_timestamp ON signals(timestamp);
	`

	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	// Schema Migration: Add 'mint' column to signals table if it doesn't exist
	// We ignore the error because if the column exists, it will fail, which is fine.
	// In a production app we'd query PRAGMA table_info.
	db.Exec(`ALTER TABLE signals ADD COLUMN mint TEXT DEFAULT ""`)
	// Migration: Add reached_2x to positions
	db.Exec(`ALTER TABLE positions ADD COLUMN reached_2x BOOLEAN DEFAULT 0`)

	return nil
}

// InsertPosition inserts or replaces a position
func (d *DB) InsertPosition(p *Position) error {
	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO positions 
		(mint, token_name, size, entry_value, entry_unit, entry_time, entry_tx_sig, msg_id, reached_2x)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Mint, p.TokenName, p.Size, p.EntryValue, p.EntryUnit, p.EntryTime, p.EntryTxSig, p.MsgID, p.Reached2X)
	return err
}

// DeletePosition removes a position
func (d *DB) DeletePosition(mint string) error {
	_, err := d.db.Exec("DELETE FROM positions WHERE mint = ?", mint)
	return err
}

// GetPosition retrieves a position by mint
func (d *DB) GetPosition(mint string) (*Position, error) {
	var p Position
	err := d.db.QueryRow(`
		SELECT mint, token_name, size, entry_value, entry_unit, entry_time, entry_tx_sig, msg_id, COALESCE(reached_2x, 0)
		FROM positions WHERE mint = ?`, mint).Scan(
		&p.Mint, &p.TokenName, &p.Size, &p.EntryValue, &p.EntryUnit, &p.EntryTime, &p.EntryTxSig, &p.MsgID, &p.Reached2X)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetAllPositions retrieves all open positions
func (d *DB) GetAllPositions() ([]*Position, error) {
	rows, err := d.db.Query(`
		SELECT mint, token_name, size, entry_value, entry_unit, entry_time, entry_tx_sig, msg_id, COALESCE(reached_2x, 0)
		FROM positions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var positions []*Position
	for rows.Next() {
		var p Position
		if err := rows.Scan(&p.Mint, &p.TokenName, &p.Size, &p.EntryValue, &p.EntryUnit, &p.EntryTime, &p.EntryTxSig, &p.MsgID, &p.Reached2X); err != nil {
			return nil, err
		}
		positions = append(positions, &p)
	}
	return positions, rows.Err()
}

// InsertTrade logs a completed trade
func (d *DB) InsertTrade(t *Trade) error {
	_, err := d.db.Exec(`
		INSERT INTO trades 
		(mint, token_name, side, amount_sol, entry_value, exit_value, pnl, duration, entry_tx_sig, exit_tx_sig, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Mint, t.TokenName, t.Side, t.AmountSol, t.EntryValue, t.ExitValue, t.PnL, t.Duration, t.EntryTxSig, t.ExitTxSig, t.Timestamp)
	return err
}

// GetRecentTrades retrieves the most recent trades
func (d *DB) GetRecentTrades(limit int) ([]*Trade, error) {
	rows, err := d.db.Query(`
		SELECT id, mint, token_name, side, amount_sol, entry_value, exit_value, pnl, duration, entry_tx_sig, exit_tx_sig, timestamp
		FROM trades ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trades []*Trade
	for rows.Next() {
		var t Trade
		if err := rows.Scan(&t.ID, &t.Mint, &t.TokenName, &t.Side, &t.AmountSol, &t.EntryValue, &t.ExitValue, &t.PnL, &t.Duration, &t.EntryTxSig, &t.ExitTxSig, &t.Timestamp); err != nil {
			return nil, err
		}
		trades = append(trades, &t)
	}
	return trades, rows.Err()
}

// InsertSignal logs a signal
func (d *DB) InsertSignal(s *Signal) error {
	// Try to insert with mint column
	_, err := d.db.Exec(`
		INSERT INTO signals (token_name, value, unit, signal_type, msg_id, timestamp, mint)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.TokenName, s.Value, s.Unit, s.SignalType, s.MsgID, s.Timestamp, s.Mint)
	return err
}

// GetRecentSignals retrieves the most recent signals
func (d *DB) GetRecentSignals(limit int) ([]*Signal, error) {
	rows, err := d.db.Query(`
		SELECT id, token_name, value, unit, signal_type, msg_id, timestamp, COALESCE(mint, '')
		FROM signals ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var signals []*Signal
	for rows.Next() {
		var s Signal
		if err := rows.Scan(&s.ID, &s.TokenName, &s.Value, &s.Unit, &s.SignalType, &s.MsgID, &s.Timestamp, &s.Mint); err != nil {
			return nil, err
		}
		signals = append(signals, &s)
	}
	return signals, rows.Err()
}

// GetTradingStats returns aggregate trading stats
func (d *DB) GetTradingStats() (totalTrades int, winRate float64, totalPnL float64, err error) {
	var wins int
	err = d.db.QueryRow(`
		SELECT 
			COUNT(*) as total,
			SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) as wins,
			COALESCE(SUM(pnl), 0) as total_pnl
		FROM trades`).Scan(&totalTrades, &wins, &totalPnL)
	if err != nil {
		return
	}
	if totalTrades > 0 {
		winRate = float64(wins) / float64(totalTrades) * 100
	}
	return
}

// Phase 8: SaveBotStat saves a bot statistic (e.g., "total_entries", "reached_2x")
func (d *DB) SaveBotStat(key string, value int) error {
	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO bot_stats (key, value, updated_at)
		VALUES (?, ?, ?)`, key, value, time.Now().Unix())
	return err
}

// Phase 8: LoadBotStat loads a bot statistic, returns 0 if not found
func (d *DB) LoadBotStat(key string) int {
	var value int
	err := d.db.QueryRow(`SELECT value FROM bot_stats WHERE key = ?`, key).Scan(&value)
	if err != nil {
		return 0
	}
	return value
}

// Phase 8: LoadAllBotStats loads all bot stats into a map
func (d *DB) LoadAllBotStats() map[string]int {
	stats := make(map[string]int)
	rows, err := d.db.Query(`SELECT key, value FROM bot_stats`)
	if err != nil {
		return stats
	}
	defer rows.Close()

	for rows.Next() {
		var key string
		var value int
		if err := rows.Scan(&key, &value); err == nil {
			stats[key] = value
		}
	}
	return stats
}

// Close closes the database
func (d *DB) Close() error {
	return d.db.Close()
}

// Now returns current Unix timestamp (helper)
func Now() int64 {
	return time.Now().Unix()
}

// LoadSeenEntries loads mints that had an ENTRY signal in the last 24h
func (d *DB) LoadSeenEntries() (map[string]bool, error) {
	cutoff := time.Now().Add(-24 * time.Hour).Unix()

	rows, err := d.db.Query(`
		SELECT DISTINCT mint 
		FROM signals 
		WHERE signal_type = 'ENTRY' AND timestamp > ? AND mint != ''`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[string]bool)
	for rows.Next() {
		var mint string
		if err := rows.Scan(&mint); err != nil {
			continue
		}
		seen[mint] = true
	}

	return seen, rows.Err()
}
