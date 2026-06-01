// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/acp283"
)

var (
	errRemoteMinPriceExponentNil = errors.New("remote min price exponent should not be nil")
	errIncorrectMinPriceExponent = errors.New("incorrect min price exponent")
	errParentMinPriceExponentNil = errors.New("parent min price exponent should not be nil")
)

// MinPriceExponent computes the child block's min price exponent.
func MinPriceExponent(
	config *extras.ChainConfig,
	parent *types.Header,
	timestamp uint64,
	desired *acp283.PriceExponent,
) (*acp283.PriceExponent, error) {
	if !config.IsHelicon(timestamp) {
		return nil, nil
	}
	exponent, err := minPriceExponent(config, parent, desired)
	if err != nil {
		return nil, err
	}
	return &exponent, nil
}

// VerifyMinPriceExponent rejects a header whose MinPriceExponent could not have
// been reached from the parent in a single block.
func VerifyMinPriceExponent(
	config *extras.ChainConfig,
	parent *types.Header,
	header *types.Header,
) error {
	if !config.IsHelicon(header.Time) {
		return nil
	}

	remote := customtypes.GetHeaderExtra(header).MinPriceExponent
	if remote == nil {
		return fmt.Errorf("%w: %s", errRemoteMinPriceExponentNil, header.Hash())
	}

	// Recomputing with the claimed value as desired is idempotent when the
	// claim is reachable from parent in one step. A cheated claim gets clamped,
	// so equality proves correctness.
	expected, err := minPriceExponent(config, parent, remote)
	if err != nil {
		return err
	}
	if *remote != expected {
		return fmt.Errorf("%w: expected %d, found %d",
			errIncorrectMinPriceExponent,
			expected,
			*remote,
		)
	}
	return nil
}

// minPriceExponent moves the parent value toward desired, clamped by acp283's
// per-block step. Assumes Helicon is active for the child.
func minPriceExponent(
	config *extras.ChainConfig,
	parent *types.Header,
	desired *acp283.PriceExponent,
) (acp283.PriceExponent, error) {
	exponent := acp283.InitialPriceExponent
	if config.IsHelicon(parent.Time) {
		parentExponent := customtypes.GetHeaderExtra(parent).MinPriceExponent
		if parentExponent == nil {
			return 0, fmt.Errorf("%w: %s", errParentMinPriceExponentNil, parent.Hash())
		}
		exponent = *parentExponent
	}
	if desired != nil {
		exponent.Toward(*desired)
	}
	return exponent, nil
}
