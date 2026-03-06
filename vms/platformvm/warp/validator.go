// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	ErrUnknownValidator = errors.New("unknown validator")
	ErrWeightOverflow   = errors.New("weight overflowed")
)

// ValidatorState defines the functions that must be implemented to get
// the canonical validator set for warp message validation.
type ValidatorState interface {
	GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error)
}

type (
	// Deprecated: use [validators.WarpSet] instead.
	CanonicalValidatorSet = validators.WarpSet
	// Deprecated: use [validators.Warp] instead.
	Validator = validators.Warp
)

// Deprecated: use [validators.FlattenValidatorSet] instead.
var FlattenValidatorSet = validators.FlattenValidatorSet

// GetCanonicalValidatorSetFromSubnetID returns the CanonicalValidatorSet of [subnetID] at
// [pChainHeight]. The returned CanonicalValidatorSet includes the validator set in a canonical ordering
// and the total weight.
//
// Deprecated: Use [validators.State.GetWarpValidatorSet] instead.
func GetCanonicalValidatorSetFromSubnetID(
	ctx context.Context,
	pChainState ValidatorState,
	pChainHeight uint64,
	subnetID ids.ID,
) (validators.WarpSet, error) {
	// Get the validator set at the given height.
	vdrSet, err := pChainState.GetValidatorSet(ctx, pChainHeight, subnetID)
	if err != nil {
		return validators.WarpSet{}, err
	}

	// Convert the validator set into the canonical ordering.
	return validators.FlattenValidatorSet(vdrSet)
}

// FilterValidators returns the validators in [vdrs] whose bit is set to 1 in
// [indices].
//
// Returns an error if [indices] references an unknown validator.
func FilterValidators(
	indices set.Bits,
	vdrs []*validators.Warp,
) ([]*validators.Warp, error) {
	// Verify that all alleged signers exist
	if indices.BitLen() > len(vdrs) {
		return nil, fmt.Errorf(
			"%w: NumIndices (%d) >= NumFilteredValidators (%d)",
			ErrUnknownValidator,
			indices.BitLen()-1, // -1 to convert from length to index
			len(vdrs),
		)
	}

	filteredVdrs := make([]*validators.Warp, 0, len(vdrs))
	for i, vdr := range vdrs {
		if !indices.Contains(i) {
			continue
		}

		filteredVdrs = append(filteredVdrs, vdr)
	}
	return filteredVdrs, nil
}

// SumWeight returns the total weight of the provided validators.
func SumWeight(vdrs []*validators.Warp) (uint64, error) {
	var (
		weight uint64
		err    error
	)
	for _, vdr := range vdrs {
		weight, err = math.Add(weight, vdr.Weight)
		if err != nil {
			return 0, fmt.Errorf("%w: %w", ErrWeightOverflow, err)
		}
	}
	return weight, nil
}

// AggregatePublicKeys returns the public key of the provided validators.
//
// Invariant: All of the public keys in [vdrs] are valid.
func AggregatePublicKeys(vdrs []*validators.Warp) (*bls.PublicKey, error) {
	pks := make([]*bls.PublicKey, len(vdrs))
	for i, vdr := range vdrs {
		pks[i] = vdr.PublicKey()
	}
	return bls.AggregatePublicKeys(pks)
}

// GetCanonicalValidatorSetFromChainID returns the canonical validator set given a validators.State, pChain height and a sourceChainID.
func GetCanonicalValidatorSetFromChainID(ctx context.Context,
	pChainState validators.State,
	pChainHeight uint64,
	sourceChainID ids.ID,
) (validators.WarpSet, error) {
	subnetID, err := pChainState.GetSubnetID(ctx, sourceChainID)
	if err != nil {
		return validators.WarpSet{}, err
	}

	return GetCanonicalValidatorSetFromSubnetID(ctx, pChainState, pChainHeight, subnetID)
}
