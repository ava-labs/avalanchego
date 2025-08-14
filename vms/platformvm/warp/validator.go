// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	_ utils.Sortable[*Validator] = (*Validator)(nil)

	ErrUnknownValidator = errors.New("unknown validator")
	ErrWeightOverflow   = errors.New("weight overflowed")
)

// ValidatorState defines the functions that must be implemented to get
// the canonical validator set for warp message validation.
type ValidatorState interface {
	GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error)
}

type CanonicalValidatorSet struct {
	// Validators slice in canonical ordering of the validators that has public key
	Validators []*Validator
	// The total weight of all the validators, including the ones that doesn't have a public key
	TotalWeight uint64
}

type Validator struct {
	PublicKey      *bls.PublicKey
	PublicKeyBytes []byte
	Weight         uint64
	NodeIDs        []ids.NodeID
}

func (v *Validator) Compare(o *Validator) int {
	return bytes.Compare(v.PublicKeyBytes, o.PublicKeyBytes)
}

// GetCanonicalValidatorSetFromSubnetID returns the CanonicalValidatorSet of [subnetID] at
// [pChcainHeight]. The returned CanonicalValidatorSet includes the validator set in a canonical ordering
// and the total weight.
func GetCanonicalValidatorSetFromSubnetID(
	ctx context.Context,
	pChainState ValidatorState,
	pChainHeight uint64,
	subnetID ids.ID,
) (CanonicalValidatorSet, error) {
	// Get the validator set at the given height.
	vdrSet, err := pChainState.GetValidatorSet(ctx, pChainHeight, subnetID)
	if err != nil {
		return CanonicalValidatorSet{}, err
	}

	// Convert the validator set into the canonical ordering.
	return FlattenValidatorSet(vdrSet)
}

// FlattenValidatorSet converts the provided [vdrSet] into a canonical ordering.
// Also returns the total weight of the validator set.
func FlattenValidatorSet(vdrSet map[ids.NodeID]*validators.GetValidatorOutput) (CanonicalValidatorSet, error) {
	var (
		vdrs        = make(map[string]*Validator, len(vdrSet))
		totalWeight uint64
		err         error
	)
	for _, vdr := range vdrSet {
		totalWeight, err = math.Add(totalWeight, vdr.Weight)
		if err != nil {
			return CanonicalValidatorSet{}, fmt.Errorf("%w: %w", ErrWeightOverflow, err)
		}

		if vdr.PublicKey == nil {
			continue
		}

		pkBytes := bls.PublicKeyToUncompressedBytes(vdr.PublicKey)
		uniqueVdr, ok := vdrs[string(pkBytes)]
		if !ok {
			uniqueVdr = &Validator{
				PublicKey:      vdr.PublicKey,
				PublicKeyBytes: pkBytes,
			}
			vdrs[string(pkBytes)] = uniqueVdr
		}

		uniqueVdr.Weight += vdr.Weight // Impossible to overflow here
		uniqueVdr.NodeIDs = append(uniqueVdr.NodeIDs, vdr.NodeID)
	}

	// Sort validators by public key
	vdrList := maps.Values(vdrs)
	utils.Sort(vdrList)
	return CanonicalValidatorSet{Validators: vdrList, TotalWeight: totalWeight}, nil
}

// FilterValidators returns the validators in [vdrs] whose bit is set to 1 in
// [indices].
//
// Returns an error if [indices] references an unknown validator.
func FilterValidators(
	indices set.Bits,
	vdrs []*Validator,
) ([]*Validator, error) {
	// Verify that all alleged signers exist
	if indices.BitLen() > len(vdrs) {
		return nil, fmt.Errorf(
			"%w: NumIndices (%d) >= NumFilteredValidators (%d)",
			ErrUnknownValidator,
			indices.BitLen()-1, // -1 to convert from length to index
			len(vdrs),
		)
	}

	filteredVdrs := make([]*Validator, 0, len(vdrs))
	for i, vdr := range vdrs {
		if !indices.Contains(i) {
			continue
		}

		filteredVdrs = append(filteredVdrs, vdr)
	}
	return filteredVdrs, nil
}

// SumWeight returns the total weight of the provided validators.
func SumWeight(vdrs []*Validator) (uint64, error) {
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
func AggregatePublicKeys(vdrs []*Validator) (*bls.PublicKey, error) {
	pks := make([]*bls.PublicKey, len(vdrs))
	for i, vdr := range vdrs {
		pks[i] = vdr.PublicKey
	}
	return bls.AggregatePublicKeys(pks)
}

// GetCanonicalValidatorSetFromChainID returns the canonical validator set given a validators.State, pChain height and a sourceChainID.
func GetCanonicalValidatorSetFromChainID(ctx context.Context,
	pChainState validators.State,
	pChainHeight uint64,
	sourceChainID ids.ID,
) (CanonicalValidatorSet, error) {
	subnetID, err := pChainState.GetSubnetID(ctx, sourceChainID)
	if err != nil {
		return CanonicalValidatorSet{}, err
	}

	return GetCanonicalValidatorSetFromSubnetID(ctx, pChainState, pChainHeight, subnetID)
}
