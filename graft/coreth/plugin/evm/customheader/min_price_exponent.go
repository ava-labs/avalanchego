// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
)

var (
	errRemoteMinPriceExponentSet = errors.New("remote min price exponent should be nil")
	errRemoteMinPriceExponentNil = errors.New("remote min price exponent should not be nil")
	errIncorrectMinPriceExponent = errors.New("incorrect min price exponent")
)

// MinPriceExponent computes the child block's min price exponent.
func MinPriceExponent(
	config *extras.ChainConfig,
	parent *types.Header,
	timestamp uint64,
	desired *dynamic.PriceExponent,
) *dynamic.PriceExponent {
	if !config.IsHelicon(timestamp) {
		return nil
	}
	return utils.PointerTo(minPriceExponent(parent, desired))
}

// VerifyMinPriceExponent rejects a header whose MinPriceExponent could not have
// been reached from the parent in a single block.
func VerifyMinPriceExponent(
	config *extras.ChainConfig,
	parent *types.Header,
	header *types.Header,
) error {
	remote := customtypes.GetHeaderExtra(header).MinPriceExponent
	if !config.IsHelicon(header.Time) {
		// TODO(powerslider): remove after Helicon is activated.
		if remote != nil {
			return fmt.Errorf("%w: %s", errRemoteMinPriceExponentSet, header.Hash())
		}
		return nil
	}

	if remote == nil {
		return fmt.Errorf("%w: %s", errRemoteMinPriceExponentNil, header.Hash())
	}

	// Recomputing with the claimed value as desired is idempotent when the
	// claim is reachable from parent in one step. A cheated claim gets clamped,
	// so equality proves correctness.
	expected := minPriceExponent(parent, remote)
	if *remote != expected {
		return fmt.Errorf("%w: expected %d, found %d",
			errIncorrectMinPriceExponent,
			expected,
			*remote,
		)
	}
	return nil
}

// minPriceExponent moves the parent's exponent toward desired, clamped by
// ACP-283's per-block step. A pre-Helicon parent carries no exponent, so the
// child starts from the initial value. Toward treats a nil desired as no-op.
func minPriceExponent(parent *types.Header, desired *dynamic.PriceExponent) dynamic.PriceExponent {
	exponent := dynamic.InitialPriceExponent
	if parentExponent := customtypes.GetHeaderExtra(parent).MinPriceExponent; parentExponent != nil {
		exponent = *parentExponent
	}
	return exponent.Toward(desired)
}
