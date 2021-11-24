// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package verify

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

var (
	errSameChainID         = errors.New("same chainID")
	errMismatchedSubnetIDs = errors.New("mismatched subnetIDs")
)

// SameSubnet verifies that the provided [ctx] was provided to a chain in the
// same subnet as [peerChainID], but not the same chain. If this verification
// fails, a non-nil error will be returned.
func SameSubnet(ctx *snow.Context, peerChainID ids.ID) error {
	if peerChainID == ctx.ChainID {
		return errSameChainID
	}

	subnetID, err := ctx.SNLookup.SubnetID(peerChainID)
	if err != nil {
		return fmt.Errorf("failed to get subnet of %q: %w", peerChainID, err)
	}
	if ctx.SubnetID != subnetID {
		return fmt.Errorf("%w; expected %q got %q", errMismatchedSubnetIDs, ctx.SubnetID, subnetID)
	}
	return nil
}
