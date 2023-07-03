// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package utxo

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type caminoHandler struct {
	handler
	lockModeBondDeposit bool
}

func NewCaminoHandler(
	ctx *snow.Context,
	clk *mockable.Clock,
	fx fx.Fx,
	lockModeBondDeposit bool,
) Handler {
	return &caminoHandler{
		handler: handler{
			ctx: ctx,
			clk: clk,
			fx:  fx,
		},
		lockModeBondDeposit: lockModeBondDeposit,
	}
}

func (h *caminoHandler) Spend(
	utxoDB avax.UTXOReader,
	keys []*secp256k1.PrivateKey,
	amount uint64,
	fee uint64,
	changeAddr ids.ShortID,
) (
	[]*avax.TransferableInput, // inputs
	[]*avax.TransferableOutput, // returnedOutputs
	[]*avax.TransferableOutput, // stakedOutputs
	[][]*secp256k1.PrivateKey, // signers
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
		inputs, outputs, signers, _, err := h.Lock(utxoDB, keys, amount, fee, locked.StateUnlocked, nil, change, 0)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		return inputs, outputs, []*avax.TransferableOutput{}, signers, nil
	}
	return h.handler.Spend(utxoDB, keys, amount, fee, changeAddr)
}

func (h *caminoHandler) VerifySpend(
	tx txs.UnsignedTx,
	utxoDB avax.UTXOGetter,
	ins []*avax.TransferableInput,
	outs []*avax.TransferableOutput,
	creds []verify.Verifiable,
	unlockedProduced map[ids.ID]uint64,
) error {
	if h.lockModeBondDeposit {
		burnedAmount, err := h.toAVAXBurnedAmount(unlockedProduced)
		if err != nil {
			return err
		}

		return h.VerifyLock(
			tx,
			utxoDB,
			ins,
			outs,
			creds,
			0,
			burnedAmount,
			h.ctx.AVAXAssetID,
			locked.StateUnlocked,
		)
	}
	return h.handler.VerifySpend(tx, utxoDB, ins, outs, creds, unlockedProduced)
}

func (h *caminoHandler) VerifySpendUTXOs(
	utxoDB avax.UTXOGetter,
	tx txs.UnsignedTx,
	utxos []*avax.UTXO,
	ins []*avax.TransferableInput,
	outs []*avax.TransferableOutput,
	creds []verify.Verifiable,
	unlockedProduced map[ids.ID]uint64,
) error {
	if h.lockModeBondDeposit {
		burnedAmount, err := h.toAVAXBurnedAmount(unlockedProduced)
		if err != nil {
			return err
		}

		msigState, ok := utxoDB.(secp256k1fx.AliasGetter)
		if !ok {
			return secp256k1fx.ErrNotAliasGetter
		}

		return h.VerifyLockUTXOs(
			msigState,
			tx,
			utxos,
			ins,
			outs,
			creds,
			0,
			burnedAmount,
			h.ctx.AVAXAssetID,
			locked.StateUnlocked,
		)
	}
	return h.handler.VerifySpendUTXOs(utxoDB, tx, utxos, ins, outs, creds, unlockedProduced)
}

func (h *caminoHandler) toAVAXBurnedAmount(unlockedProduced map[ids.ID]uint64) (uint64, error) {
	if len(unlockedProduced) > 1 {
		return 0, errors.New("to many burned assets")
	}

	burnedAmount, ok := unlockedProduced[h.ctx.AVAXAssetID]

	if len(unlockedProduced) == 1 && !ok {
		return 0, errors.New("wrong burned asset")
	}

	return burnedAmount, nil
}
