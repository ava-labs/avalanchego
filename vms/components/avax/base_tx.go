// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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
	ErrWrongNetworkID = errors.New("tx has wrong network ID")

	errNilTx        = errors.New("nil tx is not valid")
	errWrongChainID = errors.New("tx has wrong chain ID")
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

// ConsumedAssetIDs returns the IDs of the assets this transaction consumes
func (t *BaseTx) ConsumedAssetIDs() ids.Set {
	assets := ids.Set{}
	for _, in := range t.Ins {
		assets.Add(in.AssetID())
	}
	return assets
}

// AssetIDs returns the IDs of the assets this transaction depends on
func (t *BaseTx) AssetIDs() ids.Set { return t.ConsumedAssetIDs() }

// NumCredentials returns the number of expected credentials
func (t *BaseTx) NumCredentials() int { return len(t.Ins) }

// Verify ensures that transaction metadata is valid
func (t *BaseTx) Verify(ctx *snow.Context) error {
	switch {
	case t == nil:
		return errNilTx
	case t.NetworkID != ctx.NetworkID:
		return ErrWrongNetworkID
	case t.BlockchainID != ctx.ChainID:
		return errWrongChainID
	case len(t.Memo) > MaxMemoSize:
		return fmt.Errorf("memo length, %d, exceeds maximum memo length, %d",
			len(t.Memo), MaxMemoSize)
	default:
		return nil
	}
}
