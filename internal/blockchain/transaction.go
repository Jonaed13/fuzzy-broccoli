package blockchain

import (
	"encoding/base64"
	"encoding/binary"

	"github.com/mr-tron/base58"
)

// ComputeBudgetProgram is the compute budget program ID
const ComputeBudgetProgramID = "ComputeBudget111111111111111111111111111111"

// TransactionBuilder builds Solana transactions
type TransactionBuilder struct {
	wallet         *Wallet
	blockhashCache *BlockhashCache
	priorityFeeLamports uint64
	computeUnitLimit    uint32
}

// NewTransactionBuilder creates a new transaction builder
func NewTransactionBuilder(wallet *Wallet, blockhashCache *BlockhashCache, priorityFeeLamports uint64) *TransactionBuilder {
	return &TransactionBuilder{
		wallet:              wallet,
		blockhashCache:      blockhashCache,
		priorityFeeLamports: priorityFeeLamports,
		computeUnitLimit:    600000, // Default for Jupiter swaps (bumped for reliability)
	}
}

// SetComputeUnitLimit sets the compute unit limit
func (b *TransactionBuilder) SetComputeUnitLimit(limit uint32) {
	b.computeUnitLimit = limit
}

// BuildComputeBudgetInstructions creates the compute budget instructions
func (b *TransactionBuilder) BuildComputeBudgetInstructions() (setLimit []byte, setPrice []byte) {
	// SetComputeUnitLimit instruction (instruction type 2)
	// Format: [1 byte instruction type] [4 bytes limit]
	setLimit = make([]byte, 5)
	setLimit[0] = 2 // SetComputeUnitLimit
	binary.LittleEndian.PutUint32(setLimit[1:], b.computeUnitLimit)

	// SetComputeUnitPrice instruction (instruction type 3)
	// Format: [1 byte instruction type] [8 bytes microLamports per CU]
	// Calculate: priorityFeeLamports / computeUnitLimit = microLamports per CU
	microLamportsPerCU := (b.priorityFeeLamports * 1_000_000) / uint64(b.computeUnitLimit)
	
	setPrice = make([]byte, 9)
	setPrice[0] = 3 // SetComputeUnitPrice
	binary.LittleEndian.PutUint64(setPrice[1:], microLamportsPerCU)

	return setLimit, setPrice
}

// ComputeBudgetProgramIDBytes returns the compute budget program ID as bytes
func ComputeBudgetProgramIDBytes() []byte {
	bytes, _ := base58.Decode(ComputeBudgetProgramID)
	return bytes
}

// SignSerializedTransaction signs a base64-encoded transaction from Jupiter
func (b *TransactionBuilder) SignSerializedTransaction(serializedTxBase64 string) (string, error) {
	// Decode the transaction
	txBytes, err := base64.StdEncoding.DecodeString(serializedTxBase64)
	if err != nil {
		return "", err
	}

	// Solana versioned transaction format:
	// [signature count] [signatures...] [message]
	// We need to sign the message and prepend our signature

	// For Jupiter swap transactions, they are typically versioned (v0)
	// The message starts after the signature section
	
	// Find message portion (skip signature count and placeholder signatures)
	// First byte is signature count in compact-u16 format
	sigCount := int(txBytes[0])
	if sigCount == 0 {
		// Message starts at byte 1
		message := txBytes[1:]
		signature := b.wallet.Sign(message)
		
		// Build signed transaction: [1 sig count][signature][message]
		signedTx := make([]byte, 1+64+len(message))
		signedTx[0] = 1 // 1 signature
		copy(signedTx[1:65], signature)
		copy(signedTx[65:], message)
		
		return base64.StdEncoding.EncodeToString(signedTx), nil
	}

	// If there are already signatures, we need to fill in ours
	// Position 0: signature count (1 byte for counts < 128)
	// Position 1-64: first signature slot (64 bytes)
	// After that: more signatures and then the message

	sigOffset := 1 // Skip sig count byte
	messageOffset := sigOffset + sigCount*64

	// Extract message
	message := txBytes[messageOffset:]
	
	// Sign message
	signature := b.wallet.Sign(message)

	// Copy signature into first slot
	copy(txBytes[sigOffset:sigOffset+64], signature)

	return base64.StdEncoding.EncodeToString(txBytes), nil
}

// GetRecentBlockhash returns the current cached blockhash
func (b *TransactionBuilder) GetRecentBlockhash() (string, error) {
	return b.blockhashCache.Get()
}
