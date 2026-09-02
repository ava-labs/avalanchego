// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
)

var (
	errRemoteMinDelayExponentNil = errors.New("remote min delay exponent should not be nil")
	errIncorrectMinDelayExponent = errors.New("incorrect min delay exponent")
	errParentMinDelayExponentNil = errors.New("parent min delay exponent should not be nil")
)

// MinDelayExponent calculates the minimum delay exponent based on the parent,
// current header timestamp, and the desired min delay exponent.
// If desiredMinDelayExponent is nil, the parent's delay exponent is used.
func MinDelayExponent(
	config *extras.ChainConfig,
	parent *types.Header,
	timestamp uint64,
	desiredMinDelayExponent *dynamic.DelayExponent,
) (*dynamic.DelayExponent, error) {
	if config.IsGranite(timestamp) {
		minDelayExponent, err := minDelayExponent(config, parent, desiredMinDelayExponent)
		if err != nil {
			return nil, fmt.Errorf("calculating min delay exponent: %w", err)
		}
		return &minDelayExponent, nil
	}
	return nil, nil
}

// VerifyMinDelayExponent verifies that the min delay exponent in header is consistent.
func VerifyMinDelayExponent(
	config *extras.ChainConfig,
	parent *types.Header,
	header *types.Header,
) error {
	if !config.IsGranite(header.Time) {
		return nil
	}

	remoteDelayExponent := customtypes.GetHeaderExtra(header).MinDelayExponent
	if remoteDelayExponent == nil {
		return fmt.Errorf("%w: %s", errRemoteMinDelayExponentNil, header.Hash())
	}

	expectedDelayExponent, err := minDelayExponent(config, parent, remoteDelayExponent)
	if err != nil {
		return fmt.Errorf("calculating expected min delay exponent: %w", err)
	}

	if *remoteDelayExponent != expectedDelayExponent {
		return fmt.Errorf("%w: expected %d, found %d",
			errIncorrectMinDelayExponent,
			expectedDelayExponent,
			*remoteDelayExponent,
		)
	}
	return nil
}

// minDelayExponent returns the child block's delay exponent from the parent and
// the desired exponent. If desired is non-nil, moves as far as allowed toward
// it. Assumes Granite is active.
func minDelayExponent(
	config *extras.ChainConfig,
	parent *types.Header,
	desiredMinDelayExponent *dynamic.DelayExponent,
) (dynamic.DelayExponent, error) {
	delayExponent := dynamic.InitialDelayExponent
	if config.IsGranite(parent.Time) {
		parentMinDelayExponent := customtypes.GetHeaderExtra(parent).MinDelayExponent
		if parentMinDelayExponent == nil {
			return 0, fmt.Errorf("%w: %s", errParentMinDelayExponentNil, parent.Hash())
		}
		delayExponent = *parentMinDelayExponent
	}
	return delayExponent.Toward(desiredMinDelayExponent), nil
}
