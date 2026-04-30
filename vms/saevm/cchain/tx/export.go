// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

// Export is the unsigned component of a transaction that transfers assets from
// the C-Chain to either the P-Chain or the X-Chain. It modifies the C-Chain
// state and produces UTXOs in the shared memory between the C-Chain and the
// destination chain.
type Export struct {
	NetworkID        uint32                     `serialize:"true" json:"networkID"`
	BlockchainID     ids.ID                     `serialize:"true" json:"blockchainID"`
	DestinationChain ids.ID                     `serialize:"true" json:"destinationChain"`
	Ins              []Input                    `serialize:"true" json:"inputs"`
	ExportedOutputs  []*avax.TransferableOutput `serialize:"true" json:"exportedOutputs"`
}

// TODO(StephenButtolph): Remove this with its removal from the interface.
func (*Export) isUnsigned() {}

// Input identifies an account + nonce pair on the C-Chain that authorizes the
// asset and quantity to deduct.
//
// If the AssetID is AVAX, the amount will be scaled up to account for the EVM's
// higher denomination.
type Input struct {
	Address common.Address `serialize:"true" json:"address"`
	Amount  uint64         `serialize:"true" json:"amount"`
	AssetID ids.ID         `serialize:"true" json:"assetID"`
	Nonce   uint64         `serialize:"true" json:"nonce"`
}
