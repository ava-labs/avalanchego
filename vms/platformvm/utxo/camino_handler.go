// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package utxo

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type caminoHandler struct {
	handler
	lockModeBondDeposit bool
}

func NewCaminoHandler(
	ctx *snow.Context,
	clk *mockable.Clock,
	utxoReader avax.UTXOReader,
	fx fx.Fx,
	lockModeBondDeposit bool,
) Handler {
	return &caminoHandler{
		handler: handler{
			ctx:         ctx,
			clk:         clk,
			utxosReader: utxoReader,
			fx:          fx,
		},
		lockModeBondDeposit: lockModeBondDeposit,
	}
}

func (h *caminoHandler) Spend(
	keys []*crypto.PrivateKeySECP256K1R,
	amount uint64,
	fee uint64,
	changeAddr ids.ShortID,
) (
	[]*avax.TransferableInput, // inputs
	[]*avax.TransferableOutput, // returnedOutputs
	[]*avax.TransferableOutput, // stakedOutputs
	[][]*crypto.PrivateKeySECP256K1R, // signers
	error,
) {
	if h.lockModeBondDeposit {
		var change *secp256k1fx.OutputOwners
		if changeAddr != ids.ShortEmpty {
			change = &secp256k1fx.OutputOwners{
				Locktime:  0,
				Threshold: 1,
				Addrs:     []ids.ShortID{changeAddr},
			}
		}
		inputs, outputs, signers, err := h.Lock(keys, amount, fee, locked.StateUnlocked, nil, change, 0)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		return inputs, outputs, []*avax.TransferableOutput{}, signers, nil
	}
	return h.handler.Spend(keys, amount, fee, changeAddr)
}
