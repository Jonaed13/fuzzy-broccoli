package websocket

import (
	"encoding/json"
	"sync"

	"github.com/rs/zerolog/log"
)

// BalanceUpdate represents a wallet balance change
type BalanceUpdate struct {
	Address  string
	Lamports uint64
	Slot     uint64
}

// TxConfirmation represents transaction confirmation status
type TxConfirmation struct {
	Signature string
	Confirmed bool
	Error     string
	Slot      uint64
}

// WalletMonitor handles real-time wallet balance and TX confirmations
type WalletMonitor struct {
	client *Client
	
	// Wallet subscription
	walletAddr string
	walletSubID uint64
	
	// TX confirmation callbacks: signature -> callback
	txCallbacks   map[string]func(TxConfirmation)
	txSubs        map[string]uint64 // signature -> subID
	txMu          sync.RWMutex
	
	// Balance callback
	onBalance func(BalanceUpdate)
}

// NewWalletMonitor creates a wallet monitor
func NewWalletMonitor(client *Client, walletAddr string) *WalletMonitor {
	return &WalletMonitor{
		client:      client,
		walletAddr:  walletAddr,
		txCallbacks: make(map[string]func(TxConfirmation)),
		txSubs:      make(map[string]uint64),
	}
}

// OnBalanceUpdate registers balance update callback
func (w *WalletMonitor) OnBalanceUpdate(handler func(BalanceUpdate)) {
	w.onBalance = handler
}

// StartWalletSubscription subscribes to wallet SOL balance
func (w *WalletMonitor) StartWalletSubscription() error {
	if w.walletAddr == "" {
		return nil
	}
	
	subID, err := w.client.AccountSubscribe(w.walletAddr, func(data json.RawMessage) {
		w.handleBalanceUpdate(data)
	})
	if err != nil {
		return err
	}
	
	w.walletSubID = subID
	log.Info().
		Str("addr", truncateStr(w.walletAddr, 8)).
		Uint64("subID", subID).
		Msg("subscribed to wallet balance")
	
	return nil
}

// handleBalanceUpdate processes wallet balance changes
func (w *WalletMonitor) handleBalanceUpdate(data json.RawMessage) {
	var update struct {
		Context struct {
			Slot uint64 `json:"slot"`
		} `json:"context"`
		Value struct {
			Lamports uint64 `json:"lamports"`
		} `json:"value"`
	}
	
	if err := json.Unmarshal(data, &update); err != nil {
		log.Warn().Err(err).Msg("failed to parse balance update")
		return
	}
	
	balUpdate := BalanceUpdate{
		Address:  w.walletAddr,
		Lamports: update.Value.Lamports,
		Slot:     update.Context.Slot,
	}
	
	log.Debug().
		Uint64("lamports", balUpdate.Lamports).
		Float64("sol", float64(balUpdate.Lamports)/1e9).
		Msg("wallet balance update")
	
	if w.onBalance != nil {
		go w.onBalance(balUpdate)
	}
}

// WaitForConfirmation subscribes to a TX signature and calls callback on confirmation
func (w *WalletMonitor) WaitForConfirmation(signature string, callback func(TxConfirmation)) error {
	w.txMu.Lock()
	defer w.txMu.Unlock()
	
	// Store callback
	w.txCallbacks[signature] = callback
	
	// Subscribe to signature
	subID, err := w.client.SignatureSubscribe(signature, func(data json.RawMessage) {
		w.handleTxConfirmation(signature, data)
	})
	if err != nil {
		delete(w.txCallbacks, signature)
		return err
	}
	
	w.txSubs[signature] = subID
	
	log.Debug().
		Str("sig", truncateStr(signature, 12)).
		Uint64("subID", subID).
		Msg("waiting for TX confirmation")
	
	return nil
}

// handleTxConfirmation processes signature confirmation notifications
func (w *WalletMonitor) handleTxConfirmation(signature string, data json.RawMessage) {
	var update struct {
		Context struct {
			Slot uint64 `json:"slot"`
		} `json:"context"`
		Value struct {
			Err interface{} `json:"err"` // null if success, object if error
		} `json:"value"`
	}
	
	if err := json.Unmarshal(data, &update); err != nil {
		log.Warn().Err(err).Msg("failed to parse TX confirmation")
		return
	}
	
	confirmation := TxConfirmation{
		Signature: signature,
		Slot:      update.Context.Slot,
		Confirmed: update.Value.Err == nil,
	}
	
	if update.Value.Err != nil {
		errBytes, _ := json.Marshal(update.Value.Err)
		confirmation.Error = string(errBytes)
	}
	
	// Get and call callback
	w.txMu.RLock()
	callback, exists := w.txCallbacks[signature]
	w.txMu.RUnlock()
	
	if exists {
		go callback(confirmation)
		
		// Cleanup
		w.txMu.Lock()
		delete(w.txCallbacks, signature)
		if subID, ok := w.txSubs[signature]; ok {
			delete(w.txSubs, signature)
			go w.client.Unsubscribe("signatureUnsubscribe", subID)
		}
		w.txMu.Unlock()
	}
	
	if confirmation.Confirmed {
		log.Info().
			Str("sig", truncateStr(signature, 12)).
			Uint64("slot", confirmation.Slot).
			Msg("✅ TX confirmed")
	} else {
		log.Error().
			Str("sig", truncateStr(signature, 12)).
			Str("error", confirmation.Error).
			Msg("❌ TX failed")
	}
}

// Stop unsubscribes from wallet and cleans up
func (w *WalletMonitor) Stop() {
	if w.walletSubID != 0 {
		w.client.Unsubscribe("accountUnsubscribe", w.walletSubID)
	}
	
	w.txMu.Lock()
	for sig, subID := range w.txSubs {
		w.client.Unsubscribe("signatureUnsubscribe", subID)
		delete(w.txSubs, sig)
		delete(w.txCallbacks, sig)
	}
	w.txMu.Unlock()
}
