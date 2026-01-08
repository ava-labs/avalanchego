// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
)

var (
	errRemoteMinDelayExcessNil = errors.New("remote min delay excess should not be nil")
	errRemoteMinDelayExcessSet = errors.New("remote min delay excess should be nil")
	errIncorrectMinDelayExcess = errors.New("incorrect min delay excess")
	errParentMinDelayExcessNil = errors.New("parent min delay excess should not be nil")
)

// MinDelayExcess calculates the minimum delay excess based on the parent, current
// header and the desired min delay excess.
// If the `desiredMinDelayExcess` is nil, the parent's delay excess is used.
func MinDelayExcess(
	config *extras.ChainConfig,
	parent *types.Header,
	timestamp uint64,
	desiredMinDelayExcess *acp226.DelayExcess,
) (*acp226.DelayExcess, error) {
	// If the header is in Granite, calculate the min delay excess.
	// Otherwise, return nil.
	if config.IsGranite(timestamp) {
		minDelayExcess, err := minDelayExcess(config, parent, desiredMinDelayExcess)
		if err != nil {
			return nil, fmt.Errorf("calculating min delay excess: %w", err)
		}
		return &minDelayExcess, nil
	}
	return nil, nil
}

// VerifyMinDelayExcess verifies that the min delay excess in header is consistent.
func VerifyMinDelayExcess(
	config *extras.ChainConfig,
	parent *types.Header,
	header *types.Header,
) error {
	isGraniteActive := config.IsGranite(header.Time)
	remoteDelayExcess := customtypes.GetHeaderExtra(header).MinDelayExcess

	switch {
	case !isGraniteActive:
		// Before Granite, min delay excess should not be set.
		if remoteDelayExcess != nil {
			return fmt.Errorf("%w: %s", errRemoteMinDelayExcessSet, header.Hash())
		}
		return nil
	case remoteDelayExcess == nil:
		return fmt.Errorf("%w: %s", errRemoteMinDelayExcessNil, header.Hash())
	}

	// By passing in the claimed excess, we ensure that the expected
	// excess is equal to the claimed excess if it is possible
	// to have correctly set it to that value. Otherwise, the resulting
	// value will be as close to the claimed value as possible, but would
	// not be equal.
	expectedDelayExcess, err := minDelayExcess(
		config,
		parent,
		remoteDelayExcess,
	)
	if err != nil {
		return fmt.Errorf("calculating expected min delay excess: %w", err)
	}

	if *remoteDelayExcess != expectedDelayExcess {
		return fmt.Errorf("%w: expected %d, found %d",
			errIncorrectMinDelayExcess,
			expectedDelayExcess,
			*remoteDelayExcess,
		)
	}
	return nil
}

// minDelayExcess takes the parent header and the desired min delay excess
// and returns the min delay excess for the child block.
// If the desired min delay excess is specified, moves the min delay excess as much
// as possible toward that desired value.
// Assumes the Granite upgrade has been activated.
func minDelayExcess(
	config *extras.ChainConfig,
	parent *types.Header,
	desiredMinDelayExcess *acp226.DelayExcess,
) (acp226.DelayExcess, error) {
	minDelayExcess := acp226.DelayExcess(acp226.InitialDelayExcess)
	if config.IsGranite(parent.Time) {
		// If the parent block was running with ACP-226, we start with the
		// resulting min delay excess from the parent block.
		parentMinDelayExcess := customtypes.GetHeaderExtra(parent).MinDelayExcess
		if parentMinDelayExcess == nil {
			return 0, fmt.Errorf("%w: %s", errParentMinDelayExcessNil, parent.Hash())
		}
		minDelayExcess = *parentMinDelayExcess
	}

	if desiredMinDelayExcess != nil {
		minDelayExcess.UpdateDelayExcess(*desiredMinDelayExcess)
	}
	return minDelayExcess, nil
}
