// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/types"
)

// MaxMemoSize is the maximum number of bytes in the memo field
const MaxMemoSize = 256

var (
	ErrNilTx          = errors.New("nil tx is not valid")
	ErrWrongNetworkID = errors.New("tx has wrong network ID")
	ErrWrongChainID   = errors.New("tx has wrong chain ID")
	ErrMemoTooLarge   = errors.New("memo exceeds maximum length")
)

// BaseTx is the basis of all standard transactions.
type BaseTx struct {
	NetworkID    uint32                `serialize:"true" json:"networkID"`    // ID of the network this chain lives on
	BlockchainID ids.ID                `serialize:"true" json:"blockchainID"` // ID of the chain on which this transaction exists (prevents replay attacks)
	Outs         []*TransferableOutput `serialize:"true" json:"outputs"`      // The outputs of this transaction
	Ins          []*TransferableInput  `serialize:"true" json:"inputs"`       // The inputs to this transaction
	Memo         types.JSONByteSlice   `serialize:"true" json:"memo"`         // Memo field contains arbitrary bytes, up to maxMemoSize
}

// InputUTXOs track which UTXOs this transaction is consuming.
func (t *BaseTx) InputUTXOs() []*UTXOID {
	utxos := make([]*UTXOID, len(t.Ins))
	for i, in := range t.Ins {
		utxos[i] = &in.UTXOID
	}
	return utxos
}

// NumCredentials returns the number of expected credentials
func (t *BaseTx) NumCredentials() int {
	return len(t.Ins)
}

// Verify ensures that transaction metadata is valid
func (t *BaseTx) Verify(ctx *snow.Context) error {
	switch {
	case t == nil:
		return ErrNilTx
	case t.NetworkID != ctx.NetworkID:
		return ErrWrongNetworkID
	case t.BlockchainID != ctx.ChainID:
		return ErrWrongChainID
	case len(t.Memo) > MaxMemoSize:
		return fmt.Errorf(
			"%w: %d > %d",
			ErrMemoTooLarge,
			len(t.Memo),
			MaxMemoSize,
		)
	default:
		return nil
	}
}

func VerifyMemoFieldLength(memo types.JSONByteSlice, isDurangoActive bool) error {
	if !isDurangoActive {
		// SyntacticVerify validates this field pre-Durango
		return nil
	}

	if len(memo) != 0 {
		return fmt.Errorf(
			"%w: %d > %d",
			ErrMemoTooLarge,
			len(memo),
			0,
		)
	}

	return nil
}
