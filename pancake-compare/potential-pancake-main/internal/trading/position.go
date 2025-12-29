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

	mu         sync.RWMutex
	LastUpdate time.Time
}

// Snapshot returns a thread-safe copy of the position
func (p *Position) Snapshot() *Position {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return &Position{
		Mint:         p.Mint,
		TokenName:    p.TokenName,
		Size:         p.Size,
		EntryValue:   p.EntryValue,
		EntryUnit:    p.EntryUnit,
		EntryTime:    p.EntryTime,
		EntryTxSig:   p.EntryTxSig,
		MsgID:        p.MsgID,
		PoolAddr:     p.PoolAddr,
		CurrentValue: p.CurrentValue,
		PnLSol:       p.PnLSol,
		PnLPercent:   p.PnLPercent,
		Reached2X:    p.Reached2X,
		PartialSold:  p.PartialSold,
		TokenBalance: p.TokenBalance,
		LastUpdate:   p.LastUpdate,
		// mu is zero value (unlocked)
	}
}

// UpdateStats updates the position statistics safely and returns the PnL multiple
func (p *Position) UpdateStats(currentValSol float64, tokenBalance uint64) float64 {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.TokenBalance = tokenBalance
	p.PnLSol = currentValSol - p.Size
	p.LastUpdate = time.Now()

	multiple := 0.0
	if p.Size > 0 {
		multiple = currentValSol / p.Size
		p.PnLPercent = (multiple - 1.0) * 100
		// Fix: Maintain CurrentValue in EntryValue units (e.g. MCAP)
		p.CurrentValue = multiple * p.EntryValue
	}
	return multiple
}

func (p *Position) SetReached2X(reached bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Reached2X = reached
}

func (p *Position) IsReached2X() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Reached2X
}

func (p *Position) SetPartialSold(sold bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.PartialSold = sold
}

func (p *Position) IsPartialSold() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.PartialSold
}

func (p *Position) SetEntryTxSig(sig string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.EntryTxSig = sig
}

func (p *Position) GetEntryTxSig() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.EntryTxSig
}

func (p *Position) GetLastUpdate() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.LastUpdate
}

func (p *Position) SetTokenBalance(balance uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.TokenBalance = balance
}

func (p *Position) SetStatsFromSignal(val float64, unit string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	realVal := val
	if unit == "X" {
		realVal = val * 100 // Convert 2.0X -> 200% for display consistency if needed
		// Note: Legacy code did this. We keep it or adapt.
		// Legacy: val = signal.Value * 100
	}
	// Legacy override logic from executeBuyFast
	p.CurrentValue = realVal
	if p.EntryValue > 0 {
		p.PnLPercent = ((p.CurrentValue / p.EntryValue) - 1) * 100
	}
	p.LastUpdate = time.Now()
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
	
	loaded := 0
	stale := 0
	
	for _, p := range positions {
		entryTime := time.Unix(p.EntryTime, 0)
		
		if time.Since(entryTime) > 24*time.Hour {
			stale++
			log.Debug().
				Str("token", p.TokenName).
				Dur("age", time.Since(entryTime)).
				Msg("skipping stale position from DB")
			continue
		}
		
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
			CurrentValue: p.EntryValue,
			PnLPercent:   0,
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
			EntryTxSig: pos.GetEntryTxSig(), // Use getter
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

// GetAll returns all open positions (live pointers)
func (pt *PositionTracker) GetAll() []*Position {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	positions := make([]*Position, 0, len(pt.positions))
	for _, p := range pt.positions {
		positions = append(positions, p)
	}
	return positions
}

// GetAllSnapshots returns thread-safe copies of all positions (for TUI)
func (pt *PositionTracker) GetAllSnapshots() []*Position {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	snaps := make([]*Position, 0, len(pt.positions))
	for _, p := range pt.positions {
		snaps = append(snaps, p.Snapshot())
	}
	return snaps
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
