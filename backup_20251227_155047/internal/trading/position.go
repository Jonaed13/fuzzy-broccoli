package trading

import (
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"solana-pump-bot/internal/storage"
)

// Position represents an active trading position
type Position struct {
	Mint       string
	TokenName  string
	Size       float64   // SOL amount spent
	EntryValue float64   // Entry signal value
	EntryUnit  string    // "%" or "X"
	EntryTime  time.Time
	EntryTxSig   string
	MsgID        int64
	PoolAddr     string // AMM pool address for price tracking
	// Dynamic fields for TUI/Tracking
	CurrentValue float64
	PnLSol       float64
	PnLPercent   float64
	Reached2X    bool
	PartialSold  bool   // True if partial profit has been taken
	TokenBalance uint64 // Real-time balance from WebSocket
}

// PositionTracker manages active positions
type PositionTracker struct {
	mu        sync.RWMutex
	positions map[string]*Position // keyed by mint
	db        *storage.DB
	maxPos    int
}

// NewPositionTracker creates a new position tracker
func NewPositionTracker(db *storage.DB, maxPositions int) *PositionTracker {
	pt := &PositionTracker{
		positions: make(map[string]*Position),
		db:        db,
		maxPos:    maxPositions,
	}

	// Load existing positions from DB
	if db != nil {
		pt.loadFromDB()
	}

	return pt
}

func (pt *PositionTracker) loadFromDB() {
	positions, err := pt.db.GetAllPositions()
	if err != nil {
		log.Error().Err(err).Msg("failed to load positions from DB")
		return
	}

	pt.mu.Lock()
	defer pt.mu.Unlock()
	
	// FIX: Track how many we skip as stale
	loaded := 0
	stale := 0
	
	for _, p := range positions {
		entryTime := time.Unix(p.EntryTime, 0)
		
		// FIX: Skip positions older than 24 hours (stale data)
		if time.Since(entryTime) > 24*time.Hour {
			stale++
			log.Debug().
				Str("token", p.TokenName).
				Dur("age", time.Since(entryTime)).
				Msg("skipping stale position from DB")
			continue
		}
		
		// Skip PENDING positions older than 10 minutes
		if p.EntryTxSig == "PENDING" && time.Since(entryTime) > 10*time.Minute {
			stale++
			log.Debug().Str("token", p.TokenName).Msg("skipping old PENDING position")
			continue
		}
		
		pt.positions[p.Mint] = &Position{
			Mint:         p.Mint,
			TokenName:    p.TokenName,
			Size:         p.Size,
			EntryValue:   p.EntryValue,
			EntryUnit:    p.EntryUnit,
			EntryTime:    entryTime,
			EntryTxSig:   p.EntryTxSig,
			MsgID:        p.MsgID,
			CurrentValue: p.EntryValue, // Initialize to entry value
			PnLPercent:   0,            // Start at 0% until updated
		}
		loaded++
	}
	
	if stale > 0 {
		log.Warn().Int("stale", stale).Int("loaded", loaded).Msg("cleaned up stale positions from DB")
	} else {
		log.Info().Int("count", loaded).Msg("loaded positions from DB")
	}
}

// Clear removes all positions from memory (does not sell)
func (pt *PositionTracker) Clear() {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.positions = make(map[string]*Position)
}

// Has checks if a position exists for a mint
func (pt *PositionTracker) Has(mint string) bool {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	_, exists := pt.positions[mint]
	return exists
}

// Get retrieves a position by mint
func (pt *PositionTracker) Get(mint string) *Position {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return pt.positions[mint]
}

// Count returns number of open positions
func (pt *PositionTracker) Count() int {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return len(pt.positions)
}

// CanOpen checks if a new position can be opened
func (pt *PositionTracker) CanOpen() bool {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return len(pt.positions) < pt.maxPos
}

// Add adds a new position
func (pt *PositionTracker) Add(pos *Position) error {
	pt.mu.Lock()
	pt.positions[pos.Mint] = pos
	pt.mu.Unlock()

	// Persist to DB
	if pt.db != nil {
		dbPos := &storage.Position{
			Mint:       pos.Mint,
			TokenName:  pos.TokenName,
			Size:       pos.Size,
			EntryValue: pos.EntryValue,
			EntryUnit:  pos.EntryUnit,
			EntryTime:  pos.EntryTime.Unix(),
			EntryTxSig: pos.EntryTxSig,
			MsgID:      pos.MsgID,
		}
		return pt.db.InsertPosition(dbPos)
	}
	return nil
}

// Remove removes a position
func (pt *PositionTracker) Remove(mint string) (*Position, error) {
	pt.mu.Lock()
	pos := pt.positions[mint]
	delete(pt.positions, mint)
	pt.mu.Unlock()

	// Persist to DB
	if pt.db != nil {
		return pos, pt.db.DeletePosition(mint)
	}
	return pos, nil
}

// GetAll returns all open positions
func (pt *PositionTracker) GetAll() []*Position {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	positions := make([]*Position, 0, len(pt.positions))
	for _, p := range pt.positions {
		positions = append(positions, p)
	}
	return positions
}

// SetMaxPositions updates the max positions limit
func (pt *PositionTracker) SetMaxPositions(max int) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.maxPos = max
}

// ClearAll removes all positions from memory and DB (F9 clear)
func (pt *PositionTracker) ClearAll() {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	
	// Clear DB
	if pt.db != nil {
		for mint := range pt.positions {
			pt.db.DeletePosition(mint)
		}
	}
	
	// Clear memory
	pt.positions = make(map[string]*Position)
	
	log.Info().Msg("all positions cleared")
}
