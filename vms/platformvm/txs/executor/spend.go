// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
)

var errFlowCheckFailed = errors.New("flow check failed")

// applySpend spends the UTXOs of tx in diff: it consumes the inputs of tx and
// produces its base outputs. Unless the node is still bootstrapping, it first
// verifies via [verifySpend] that the inputs, authorized by creds, fund the
// outputs, the fee, and extraProduced.
//
// extraProduced holds amounts that tx must fund without a corresponding
// output, such as the reward supply of a [platform.TransformSubnetTx]. It may
// be nil.
//
// It is shared by the standard and proposal executors. Callers must verify tx
// and select its spending credentials before calling applySpend. Callers must
// discard the state diff if transaction execution fails.
//
// Must not be used for [platform.ImportTx]. The inputs from
// [utxo.GetInputOutputs] include the imported inputs, which are not in diff.
func applySpend(
	backend *Backend,
	feeCalculator fee.Calculator,
	diff *state.Diff,
	tx *platform.Tx,
	creds []verify.Verifiable,
	extraProduced map[ids.ID]uint64,
) error {
	unsignedTx := tx.Unsigned
	ins, outs, producedAVAX, err := utxo.GetInputOutputs(unsignedTx)
	if err != nil {
		return fmt.Errorf("getting utxos: %w", err)
	}

	// Blocks executed while bootstrapping were already accepted by the
	// network, so their txs are known to have passed the flow check.
	if backend.Bootstrapped.Get() {
		if err := verifySpend(backend, feeCalculator, diff, unsignedTx, ins, outs, producedAVAX, extraProduced, creds); err != nil {
			return err
		}
	}

	avax.Consume(diff, ins)
	// Only the base outputs become UTXOs. outs additionally holds the outputs
	// that the flow check must account for but that are not spendable on this
	// chain, such as staked or exported outputs.
	avax.Produce(diff, tx.ID(), unsignedTx.Outputs())
	return nil
}

// verifySpend verifies that ins, authorized by creds, fund outs plus
// producedAVAX, extraProduced, and the fee of tx for the current fee
// configuration.
func verifySpend(
	backend *Backend,
	feeCalculator fee.Calculator,
	chainState state.Chain,
	tx platform.UnsignedTx,
	ins []*avax.TransferableInput,
	outs []*avax.TransferableOutput,
	producedAVAX uint64,
	extraProduced map[ids.ID]uint64,
	creds []verify.Verifiable,
) error {
	txFee, err := feeCalculator.CalculateFee(tx)
	if err != nil {
		return fmt.Errorf("calculating fee: %w", err)
	}

	producedAVAX, err = math.Add(producedAVAX, txFee)
	if err != nil {
		return fmt.Errorf("adding fee: %w", err)
	}

	produced := make(map[ids.ID]uint64, len(extraProduced)+1)
	for assetID, amount := range extraProduced {
		produced[assetID] = amount
	}
	avaxAssetID := backend.Ctx.AVAXAssetID
	produced[avaxAssetID], err = math.Add(produced[avaxAssetID], producedAVAX)
	if err != nil {
		return fmt.Errorf("adding produced AVAX: %w", err)
	}

	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		ins,
		outs,
		creds,
		produced,
	); err != nil {
		return fmt.Errorf("%w: %w", errFlowCheckFailed, err)
	}

	return nil
}
