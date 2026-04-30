// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/holiman/uint256"

	// Imported for [atomic.UnsignedImportTx.Burned] comment resolution.
	_ "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Import is the unsigned component of a transaction that transfers assets from
// either the P-Chain or the X-Chain to the C-Chain. It consumes UTXOs in the
// shared memory between the C-Chain and the source chain and increases balances
// in the C-Chain state.
type Import struct {
	NetworkID      uint32                    `serialize:"true" json:"networkID"`
	BlockchainID   ids.ID                    `serialize:"true" json:"blockchainID"`
	SourceChain    ids.ID                    `serialize:"true" json:"sourceChain"`
	ImportedInputs []*avax.TransferableInput `serialize:"true" json:"importedInputs"`
	Outs           []Output                  `serialize:"true" json:"outputs"`
}

// Output specifies an account on the C-Chain whose balance of the specified
// asset should be increased.
//
// If the AssetID is AVAX, the amount will be scaled up to account for the EVM's
// higher denomination.
type Output struct {
	Address common.Address `serialize:"true" json:"address"`
	Amount  uint64         `serialize:"true" json:"amount"`
	AssetID ids.ID         `serialize:"true" json:"assetID"`
}

// Similarly to [atomic.UnsignedImportTx.Burned], burned will error if the sum
// of the inputs exceeds MaxUint64; even if the total amount burned could be
// represented as a uint64.
//
// Because the total supply of AVAX fits in a uint64, this doesn't matter in
// practice and allows for easier fuzzing.
func (i *Import) burned(assetID ids.ID) (uint64, error) {
	var (
		burned uint64
		err    error
	)
	for _, in := range i.ImportedInputs {
		if in.Asset.ID == assetID {
			burned, err = math.Add(burned, in.In.Amount())
			if err != nil {
				return 0, err
			}
		}
	}
	for _, out := range i.Outs {
		if out.AssetID == assetID {
			burned, err = math.Sub(burned, out.Amount)
			if err != nil {
				return 0, err
			}
		}
	}
	return burned, nil
}

var errUnexpectedInputType = errors.New("unexpected input type")

func (i *Import) numSigs() (uint64, error) {
	var n uint64
	for _, in := range i.ImportedInputs {
		input, ok := in.In.(*secp256k1fx.TransferInput)
		if !ok {
			return 0, fmt.Errorf("%w: got %T ; want %T", errUnexpectedInputType, in.In, input)
		}
		n += uint64(len(input.SigIndices))
	}
	return n, nil
}

var errOverflow = errors.New("amount overflow")

func (i *Import) asOp(avaxAssetID ids.ID) (op, error) {
	mint := make(map[common.Address]uint256.Int, len(i.Outs))
	for _, out := range i.Outs {
		if out.AssetID != avaxAssetID {
			continue
		}

		var (
			total  = mint[out.Address]
			amount = scaleAVAX(out.Amount)
		)
		if _, overflow := total.AddOverflow(&total, &amount); overflow {
			return op{}, fmt.Errorf("%w: for address %s", errOverflow, out.Address)
		}
		mint[out.Address] = total
	}
	return op{
		mint: mint,
	}, nil
}
