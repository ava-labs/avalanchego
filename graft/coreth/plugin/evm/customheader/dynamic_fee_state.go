// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/acp176"
)

// feeStateBeforeBlock takes the previous header and the timestamp of its child
// block and calculates the fee state before the child block is executed.
func feeStateBeforeBlock(
	config *extras.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) (acp176.State, error) {
	if timestamp < parent.Time {
		return acp176.State{}, fmt.Errorf("%w: timestamp %d prior to parent timestamp %d",
			errInvalidTimestamp,
			timestamp,
			parent.Time,
		)
	}

	var state acp176.State
	if config.IsFortuna(parent.Time) && parent.Number.Cmp(common.Big0) != 0 {
		// If the parent block was running with ACP-176, we start with the
		// resulting fee state from the parent block. It is assumed that the
		// parent has been verified, so the claimed fee state equals the actual
		// fee state.
		var err error
		state, err = acp176.ParseState(parent.Extra)
		if err != nil {
			return acp176.State{}, fmt.Errorf("parsing parent fee state: %w", err)
		}
	}

	state.AdvanceTime(timestamp - parent.Time)
	return state, nil
}

// feeStateAfterBlock takes the previous header and returns the fee state after
// the execution of the provided child.
func feeStateAfterBlock(
	config *extras.ChainConfig,
	parent *types.Header,
	header *types.Header,
	desiredTargetExcess *gas.Gas,
) (acp176.State, error) {
	// Calculate the gas state after the parent block
	state, err := feeStateBeforeBlock(config, parent, header.Time)
	if err != nil {
		return acp176.State{}, fmt.Errorf("calculating initial fee state: %w", err)
	}

	// Consume the gas used by the block
	extDataGasUsed := customtypes.GetHeaderExtra(header).ExtDataGasUsed
	if err := state.ConsumeGas(header.GasUsed, extDataGasUsed); err != nil {
		return acp176.State{}, fmt.Errorf("advancing the fee state: %w", err)
	}

	// If the desired target excess is specified, move the target excess as much
	// as possible toward that desired value.
	if desiredTargetExcess != nil {
		state.UpdateTargetExcess(*desiredTargetExcess)
	}
	return state, nil
}
