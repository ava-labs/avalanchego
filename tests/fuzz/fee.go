// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"errors"
	"fmt"
	"slices"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
)

var errUnderpaid = errors.New("accepted tx burns less than its fee at the minimum gas price")

// checkFee returns errUnderpaid if tx burns less AVAX than calc, the fee at
// the minimum gas price, charges for it.
//
// A block charges each of its txs at one gas price: the one its fee state
// gives once the chain time advances to the block's timestamp
// (block/executor/verifier.go:80,139, state/chain_time_helpers.go:141). The
// price is never below the minimum (vms/components/gas/gas.go:104-120), and
// the fee grows with the price, so the bound holds whatever price the block
// had. No surface publishes the price per block: getFeeState reads only the
// accepted tip, and deriving each block's price from it would copy the
// executor's fee state. Within a run the price stays the minimum: the excess
// reached 62,457 gas at most over seeds 3 and 7, and the price doubles only
// at ExcessConversionConstant times ln 2, about 1.5M gas on the test network,
// so the bound is exact there. A tighter bound needs the price per block on a
// published surface.
//
// calc runs the node's own txs/fee code, so a bug in the fee formula passes.
// Proposer-only txs pay no fee and are not judged.
func checkFee(calc fee.Calculator, avaxAssetID ids.ID, tx *platform.Tx) error {
	want, err := calc.CalculateFee(tx.Unsigned)
	if errors.Is(err, fee.ErrUnsupportedTx) {
		return nil
	}
	if err != nil {
		return err
	}

	v, err := valueOf(tx.Unsigned)
	if err != nil {
		return fmt.Errorf("%w: tx %s: %w", errUnderpaid, tx.ID(), err)
	}
	got, err := v.burned(avaxAssetID)
	if err != nil {
		return fmt.Errorf("%w: tx %s: %w", errUnderpaid, tx.ID(), err)
	}
	if got < want {
		return fmt.Errorf("%w: %s %s burns %d, but its fee is %d", errUnderpaid, txName(tx.Unsigned), tx.ID(), got, want)
	}

	return nil
}

// A txValue holds the fields of one tx that consume or produce value. Every
// tx type spends its Ins and pays its Outs; a few move value through fields of
// their own, which the executor counts with them
// (vms/platformvm/utxo/verifier.go:337-535).
type txValue struct {
	ins      []*avax.TransferableInput  // Ins, and an import's ImportedInputs
	outs     []*avax.TransferableOutput // Outs, and a staker's StakeOuts
	exported []*avax.TransferableOutput // an export's ExportedOutputs
	l1       uint64                     // AVAX moved into L1 validator balances
}

var errOverspent = errors.New("tx produces more than it consumes")

// burned returns the AVAX that v consumes and does not produce. Inputs count
// as the tx declares them; the fx checks each against its UTXO
// (vms/secp256k1fx/fx.go:162).
func (v txValue) burned(avaxAssetID ids.ID) (uint64, error) {
	var consumed uint64
	for _, in := range v.ins {
		if in.AssetID() != avaxAssetID {
			continue
		}

		var err error
		if consumed, err = math.Add(consumed, in.In.Amount()); err != nil {
			return 0, err
		}
	}

	produced := v.l1
	for _, out := range slices.Concat(v.outs, v.exported) {
		if out.AssetID() != avaxAssetID {
			continue
		}

		var err error
		if produced, err = math.Add(produced, out.Output().Amount()); err != nil {
			return 0, err
		}
	}
	if produced > consumed {
		return 0, fmt.Errorf("%w: %d produced, %d consumed", errOverspent, produced, consumed)
	}

	return consumed - produced, nil
}

// valueOf returns the fields of utx that move value. A visitor, so a new tx
// type fails to compile until it says which of its fields hold value.
func valueOf(utx platform.UnsignedTx) (txValue, error) {
	var v valueVisitor
	if err := utx.Visit(&v); err != nil {
		return txValue{}, err
	}

	return v.value, nil
}

type valueVisitor struct {
	value txValue
}

var _ platform.TxVisitor = (*valueVisitor)(nil)

func (v *valueVisitor) base(tx *platform.BaseTx) {
	v.value.ins = append(v.value.ins, tx.Ins...)
	v.value.outs = append(v.value.outs, tx.Outs...)
}

func (v *valueVisitor) toL1(balance uint64) error {
	var err error
	v.value.l1, err = math.Add(v.value.l1, balance)

	return err
}

func (v *valueVisitor) AddValidatorTx(tx *platform.AddValidatorTx) error {
	v.base(&tx.BaseTx)
	v.value.outs = append(v.value.outs, tx.StakeOuts...)

	return nil
}

func (v *valueVisitor) AddSubnetValidatorTx(tx *platform.AddSubnetValidatorTx) error {
	v.base(&tx.BaseTx)

	return nil
}

func (v *valueVisitor) AddDelegatorTx(tx *platform.AddDelegatorTx) error {
	v.base(&tx.BaseTx)
	v.value.outs = append(v.value.outs, tx.StakeOuts...)

	return nil
}

func (v *valueVisitor) CreateChainTx(tx *platform.CreateChainTx) error {
	v.base(&tx.BaseTx)

	return nil
}

func (v *valueVisitor) CreateSubnetTx(tx *platform.CreateSubnetTx) error {
	v.base(&tx.BaseTx)

	return nil
}

func (v *valueVisitor) ImportTx(tx *platform.ImportTx) error {
	v.base(&tx.BaseTx)
	v.value.ins = append(v.value.ins, tx.ImportedInputs...)

	return nil
}

func (v *valueVisitor) ExportTx(tx *platform.ExportTx) error {
	v.base(&tx.BaseTx)
	v.value.exported = tx.ExportedOutputs

	return nil
}

func (*valueVisitor) AdvanceTimeTx(*platform.AdvanceTimeTx) error { return nil }

func (*valueVisitor) RewardValidatorTx(*platform.RewardValidatorTx) error { return nil }

func (v *valueVisitor) RemoveSubnetValidatorTx(tx *platform.RemoveSubnetValidatorTx) error {
	v.base(&tx.BaseTx)

	return nil
}

func (v *valueVisitor) TransformSubnetTx(tx *platform.TransformSubnetTx) error {
	v.base(&tx.BaseTx)

	return nil
}

func (v *valueVisitor) AddPermissionlessValidatorTx(tx *platform.AddPermissionlessValidatorTx) error {
	v.base(&tx.BaseTx)
	v.value.outs = append(v.value.outs, tx.StakeOuts...)

	return nil
}

func (v *valueVisitor) AddPermissionlessDelegatorTx(tx *platform.AddPermissionlessDelegatorTx) error {
	v.base(&tx.BaseTx)
	v.value.outs = append(v.value.outs, tx.StakeOuts...)

	return nil
}

func (v *valueVisitor) TransferSubnetOwnershipTx(tx *platform.TransferSubnetOwnershipTx) error {
	v.base(&tx.BaseTx)

	return nil
}

func (v *valueVisitor) BaseTx(tx *platform.BaseTx) error {
	v.base(tx)

	return nil
}

func (v *valueVisitor) ConvertSubnetToL1Tx(tx *platform.ConvertSubnetToL1Tx) error {
	v.base(&tx.BaseTx)
	for _, vdr := range tx.Validators {
		if err := v.toL1(vdr.Balance); err != nil {
			return err
		}
	}

	return nil
}

func (v *valueVisitor) RegisterL1ValidatorTx(tx *platform.RegisterL1ValidatorTx) error {
	v.base(&tx.BaseTx)

	return v.toL1(tx.Balance)
}

func (v *valueVisitor) SetL1ValidatorWeightTx(tx *platform.SetL1ValidatorWeightTx) error {
	v.base(&tx.BaseTx)

	return nil
}

func (v *valueVisitor) IncreaseL1ValidatorBalanceTx(tx *platform.IncreaseL1ValidatorBalanceTx) error {
	v.base(&tx.BaseTx)

	return v.toL1(tx.Balance)
}

func (v *valueVisitor) DisableL1ValidatorTx(tx *platform.DisableL1ValidatorTx) error {
	v.base(&tx.BaseTx)

	return nil
}

func (v *valueVisitor) AddAutoRenewedValidatorTx(tx *platform.AddAutoRenewedValidatorTx) error {
	v.base(&tx.BaseTx)
	v.value.outs = append(v.value.outs, tx.StakeOuts...)

	return nil
}

func (v *valueVisitor) SetAutoRenewedValidatorConfigTx(tx *platform.SetAutoRenewedValidatorConfigTx) error {
	v.base(&tx.BaseTx)

	return nil
}

func (*valueVisitor) RewardAutoRenewedValidatorTx(*platform.RewardAutoRenewedValidatorTx) error {
	return nil
}
