package blockchain

import (
	"strings"
)

// TxError represents a human-readable transaction error
type TxError struct {
	Code    int
	Raw     string
	Message string
	Action  string
}

func (e *TxError) Error() string {
	return e.Message
}

// ParseTxError converts RPC error to human-readable message
func ParseTxError(err error) *TxError {
	if err == nil {
		return nil
	}

	raw := err.Error()
	txErr := &TxError{Raw: raw}

	// Parse error code
	rpcErr, ok := err.(*RPCError)
	if ok {
		txErr.Code = rpcErr.Code
	}

	// Match known error patterns and translate
	switch {
	
	// Insufficient balance
	case contains(raw, "no record of a prior credit"):
		txErr.Message = "❌ INSUFFICIENT BALANCE - Wallet has 0 SOL"
		txErr.Action = "Fund wallet with SOL"
	
	case contains(raw, "insufficient funds"):
		txErr.Message = "❌ INSUFFICIENT BALANCE - Not enough SOL for trade + fees"
		txErr.Action = "Add more SOL to wallet"
	
	case contains(raw, "insufficient lamports"):
		txErr.Message = "❌ INSUFFICIENT BALANCE - Not enough lamports"
		txErr.Action = "Add more SOL to wallet"

	// Slippage / Price errors
	case contains(raw, "slippage"):
		txErr.Message = "❌ SLIPPAGE TOO HIGH - Price moved too much"
		txErr.Action = "Increase slippage_bps in config"
	
	case contains(raw, "ExceededSlippage"):
		txErr.Message = "❌ SLIPPAGE EXCEEDED - Market moved against you"
		txErr.Action = "Try again or increase slippage"

	// Blockhash expired
	case contains(raw, "blockhash not found"):
		txErr.Message = "❌ BLOCKHASH EXPIRED - Transaction took too long"
		txErr.Action = "Retry immediately"
	
	case contains(raw, "block height exceeded"):
		txErr.Message = "❌ TRANSACTION EXPIRED - Blockhash too old"
		txErr.Action = "Retry immediately"

	// Rate limiting
	case contains(raw, "429"):
		txErr.Message = "⚠️ RATE LIMITED - Too many requests"
		txErr.Action = "Wait and retry"
	
	case contains(raw, "rate limit"):
		txErr.Message = "⚠️ RATE LIMITED - RPC throttled"
		txErr.Action = "Wait 1-2 seconds and retry"

	// Account errors
	case contains(raw, "account not found"):
		txErr.Message = "❌ TOKEN ACCOUNT NOT FOUND - You may not own this token"
		txErr.Action = "Check if you have token balance"
	
	case contains(raw, "AccountNotFound"):
		txErr.Message = "❌ ACCOUNT MISSING - Required account doesn't exist"
		txErr.Action = "Token may need ATA creation"

	// Compute budget
	case contains(raw, "compute budget exceeded"):
		txErr.Message = "❌ OUT OF COMPUTE - Transaction too complex"
		txErr.Action = "Increase compute unit limit"

	// Program errors
	case contains(raw, "custom program error"):
		txErr.Message = "❌ PROGRAM ERROR - DEX rejected the swap"
		txErr.Action = "Check token liquidity"
	
	case contains(raw, "0x1"):
		txErr.Message = "❌ INSUFFICIENT FUNDS IN POOL"
		txErr.Action = "Token may have low liquidity"

	// Network errors
	case contains(raw, "connection refused"):
		txErr.Message = "❌ RPC CONNECTION FAILED"
		txErr.Action = "Check internet connection"
	
	case contains(raw, "timeout"):
		txErr.Message = "⚠️ RPC TIMEOUT - Network slow"
		txErr.Action = "Retry"

	// Simulation errors
	case contains(raw, "simulation failed"):
		txErr.Message = "❌ SIMULATION FAILED - Transaction would fail on-chain"
		txErr.Action = "Check logs for specific reason"

	// Default
	default:
		txErr.Message = "❌ TRANSACTION FAILED"
		txErr.Action = "Check raw error"
	}

	return txErr
}

// HumanError returns a human-readable error string
func HumanError(err error) string {
	if err == nil {
		return ""
	}
	txErr := ParseTxError(err)
	return txErr.Message
}

// HumanErrorWithAction returns error + suggested action
func HumanErrorWithAction(err error) string {
	if err == nil {
		return ""
	}
	txErr := ParseTxError(err)
	return txErr.Message + " → " + txErr.Action
}

func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
