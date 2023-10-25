// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

var (
	ErrSameChainID         = errors.New("same chainID")
	ErrMismatchedSubnetIDs = errors.New("mismatched subnetIDs")
)

// SameSubnet verifies that the provided [ctx] was provided to a chain in the
// same subnet as [peerChainID], but not the same chain. If this verification
// fails, a non-nil error will be returned.
func SameSubnet(ctx context.Context, ctxValidatorState validators.State, ctxSubnetID ids.ID, ctxChainID ids.ID, peerChainID ids.ID) error {
	if peerChainID == ctxChainID {
		return ErrSameChainID
	}

	subnetID, err := ctxValidatorState.GetSubnetID(ctx, peerChainID)
	if err != nil {
		return fmt.Errorf("failed to get subnet of %q: %w", peerChainID, err)
	}
	if ctxSubnetID != subnetID {
		return fmt.Errorf("%w; expected %q got %q", ErrMismatchedSubnetIDs, ctxSubnetID, subnetID)
	}
	return nil
}
