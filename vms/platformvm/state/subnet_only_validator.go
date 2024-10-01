// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	_ btree.LessFunc[SubnetOnlyValidator] = SubnetOnlyValidator.Less

	ErrMutatedSubnetOnlyValidator   = errors.New("subnet only validator contains mutated constant fields")
	ErrDuplicateSubnetOnlyValidator = errors.New("subnet only validator contains conflicting subnetID + nodeID pair")
)

type SubnetOnlyValidators interface {
	// GetActiveSubnetOnlyValidatorsIterator returns an iterator of all the
	// active subnet only validators in increasing order of EndAccumulatedFee.
	GetActiveSubnetOnlyValidatorsIterator() (iterator.Iterator[SubnetOnlyValidator], error)

	// NumActiveSubnetOnlyValidators returns the number of currently active
	// subnet only validators.
	NumActiveSubnetOnlyValidators() int

	// WeightOfSubnetOnlyValidators returns the total active and inactive weight
	// of subnet only validators on [subnetID].
	WeightOfSubnetOnlyValidators(subnetID ids.ID) (uint64, error)

	// GetSubnetOnlyValidator returns the validator with [validationID] if it
	// exists. If the validator does not exist, [err] will equal
	// [database.ErrNotFound].
	GetSubnetOnlyValidator(validationID ids.ID) (SubnetOnlyValidator, error)

	// HasSubnetOnlyValidator returns the validator with [validationID] if it
	// exists.
	HasSubnetOnlyValidator(subnetID ids.ID, nodeID ids.NodeID) (bool, error)

	// PutSubnetOnlyValidator inserts [sov] as a validator.
	//
	// If inserting this validator attempts to modify any of the constant fields
	// of the subnet only validator struct, an error will be returned.
	//
	// If inserting this validator would cause the mapping of subnetID+nodeID to
	// validationID to be non-unique, an error will be returned.
	PutSubnetOnlyValidator(sov SubnetOnlyValidator) error
}

// SubnetOnlyValidator defines an ACP-77 validator. For a given ValidationID, it
// is expected for SubnetID, NodeID, PublicKey, RemainingBalanceOwner, and
// StartTime to be constant.
type SubnetOnlyValidator struct {
	// ValidationID is not serialized because it is used as the key in the
	// database, so it doesn't need to be stored in the value.
	ValidationID ids.ID

	SubnetID ids.ID     `serialize:"true"`
	NodeID   ids.NodeID `serialize:"true"`

	// PublicKey is the uncompressed BLS public key of the validator. It is
	// guaranteed to be populated.
	PublicKey []byte `serialize:"true"`

	// RemainingBalanceOwner is the owner that will be used when returning the
	// balance of the validator after removing accrued fees.
	RemainingBalanceOwner []byte `serialize:"true"`

	// DeactivationOwner is the owner that can manually deactivate the
	// validator.
	DeactivationOwner []byte `serialize:"true"`

	// StartTime is the unix timestamp, in seconds, when this validator was
	// added to the set.
	StartTime uint64 `serialize:"true"`

	// Weight of this validator. It can be updated when the MinNonce is
	// increased. If the weight is being set to 0, the validator is being
	// removed.
	Weight uint64 `serialize:"true"`

	// MinNonce is the smallest nonce that can be used to modify this
	// validator's weight. It is initially set to 0 and is set to one higher
	// than the last nonce used. It is not valid to use nonce MaxUint64 unless
	// the weight is being set to 0, which removes the validator from the set.
	MinNonce uint64 `serialize:"true"`

	// EndAccumulatedFee is the amount of globally accumulated fees that can
	// accrue before this validator must be deactivated. It is equal to the
	// amount of fees this validator is willing to pay plus the amount of
	// globally accumulated fees when this validator started validating.
	//
	// If this value is 0, the validator is inactive.
	EndAccumulatedFee uint64 `serialize:"true"`
}

func (v SubnetOnlyValidator) Less(o SubnetOnlyValidator) bool {
	return v.Compare(o) == -1
}

// Compare determines a canonical ordering of *SubnetOnlyValidators based on
// their EndAccumulatedFees and ValidationIDs. Lower EndAccumulatedFees result
// in an earlier ordering.
func (v SubnetOnlyValidator) Compare(o SubnetOnlyValidator) int {
	switch {
	case v.EndAccumulatedFee < o.EndAccumulatedFee:
		return -1
	case o.EndAccumulatedFee < v.EndAccumulatedFee:
		return 1
	default:
		return v.ValidationID.Compare(o.ValidationID)
	}
}

// validateConstants returns true if the constants of this validator have not
// been modified.
func (v SubnetOnlyValidator) validateConstants(o SubnetOnlyValidator) bool {
	if v.ValidationID != o.ValidationID {
		return true
	}
	return v.SubnetID == o.SubnetID &&
		v.NodeID == o.NodeID &&
		bytes.Equal(v.PublicKey, o.PublicKey) &&
		bytes.Equal(v.RemainingBalanceOwner, o.RemainingBalanceOwner) &&
		bytes.Equal(v.DeactivationOwner, o.DeactivationOwner) &&
		v.StartTime == o.StartTime
}

func (v SubnetOnlyValidator) isActive() bool {
	return v.Weight != 0 && v.EndAccumulatedFee != 0
}

func getSubnetOnlyValidator(db database.KeyValueReader, validationID ids.ID) (SubnetOnlyValidator, error) {
	bytes, err := db.Get(validationID[:])
	if err != nil {
		return SubnetOnlyValidator{}, err
	}

	vdr := SubnetOnlyValidator{
		ValidationID: validationID,
	}
	if _, err := block.GenesisCodec.Unmarshal(bytes, &vdr); err != nil {
		return SubnetOnlyValidator{}, fmt.Errorf("failed to unmarshal SubnetOnlyValidator: %w", err)
	}
	return vdr, nil
}

func putSubnetOnlyValidator(db database.KeyValueWriter, vdr SubnetOnlyValidator) error {
	bytes, err := block.GenesisCodec.Marshal(block.CodecVersion, vdr)
	if err != nil {
		return fmt.Errorf("failed to marshal SubnetOnlyValidator: %w", err)
	}
	return db.Put(vdr.ValidationID[:], bytes)
}

func deleteSubnetOnlyValidator(db database.KeyValueDeleter, validationID ids.ID) error {
	return db.Delete(validationID[:])
}

type subnetIDNodeID struct {
	subnetID ids.ID
	nodeID   ids.NodeID
}

type subnetOnlyValidatorsDiff struct {
	numAddedActive      int               // May be negative
	modifiedTotalWeight map[ids.ID]uint64 // subnetID -> totalWeight
	modified            map[ids.ID]SubnetOnlyValidator
	modifiedHasNodeIDs  map[subnetIDNodeID]bool
	active              *btree.BTreeG[SubnetOnlyValidator]
}

func newSubnetOnlyValidatorsDiff() *subnetOnlyValidatorsDiff {
	return &subnetOnlyValidatorsDiff{
		modifiedTotalWeight: make(map[ids.ID]uint64),
		modified:            make(map[ids.ID]SubnetOnlyValidator),
		modifiedHasNodeIDs:  make(map[subnetIDNodeID]bool),
		active:              btree.NewG(defaultTreeDegree, SubnetOnlyValidator.Less),
	}
}

func (d *subnetOnlyValidatorsDiff) getActiveSubnetOnlyValidatorsIterator(parentIterator iterator.Iterator[SubnetOnlyValidator]) iterator.Iterator[SubnetOnlyValidator] {
	return iterator.Merge(
		SubnetOnlyValidator.Less,
		iterator.Filter(parentIterator, func(sov SubnetOnlyValidator) bool {
			_, ok := d.modified[sov.ValidationID]
			return ok
		}),
		iterator.FromTree(d.active),
	)
}

func (d *subnetOnlyValidatorsDiff) hasSubnetOnlyValidator(subnetID ids.ID, nodeID ids.NodeID) (bool, bool) {
	subnetIDNodeID := subnetIDNodeID{
		subnetID: subnetID,
		nodeID:   nodeID,
	}
	has, modified := d.modifiedHasNodeIDs[subnetIDNodeID]
	return has, modified
}

func (d *subnetOnlyValidatorsDiff) putSubnetOnlyValidator(state SubnetOnlyValidators, sov SubnetOnlyValidator) error {
	var (
		prevWeight uint64
		prevActive bool
		newActive  = sov.Weight != 0 && sov.EndAccumulatedFee != 0
	)
	switch priorSOV, err := state.GetSubnetOnlyValidator(sov.ValidationID); err {
	case nil:
		if !priorSOV.validateConstants(sov) {
			return ErrMutatedSubnetOnlyValidator
		}

		prevWeight = priorSOV.Weight
		prevActive = priorSOV.EndAccumulatedFee != 0
	case database.ErrNotFound:
		has, err := state.HasSubnetOnlyValidator(sov.SubnetID, sov.NodeID)
		if err != nil {
			return err
		}
		if has {
			return ErrDuplicateSubnetOnlyValidator
		}
	default:
		return err
	}

	switch {
	case prevWeight < sov.Weight:
		weight, err := state.WeightOfSubnetOnlyValidators(sov.SubnetID)
		if err != nil {
			return err
		}

		weight, err = safemath.Add(weight, sov.Weight-prevWeight)
		if err != nil {
			return err
		}

		d.modifiedTotalWeight[sov.SubnetID] = weight
	case prevWeight > sov.Weight:
		weight, err := state.WeightOfSubnetOnlyValidators(sov.SubnetID)
		if err != nil {
			return err
		}

		weight, err = safemath.Sub(weight, prevWeight-sov.Weight)
		if err != nil {
			return err
		}

		d.modifiedTotalWeight[sov.SubnetID] = weight
	}

	switch {
	case prevActive && !newActive:
		d.numAddedActive--
	case !prevActive && newActive:
		d.numAddedActive++
	}

	if prevSOV, ok := d.modified[sov.ValidationID]; ok {
		prevSubnetIDNodeID := subnetIDNodeID{
			subnetID: prevSOV.SubnetID,
			nodeID:   prevSOV.NodeID,
		}
		d.modifiedHasNodeIDs[prevSubnetIDNodeID] = false
		d.active.Delete(prevSOV)
	}
	d.modified[sov.ValidationID] = sov

	subnetIDNodeID := subnetIDNodeID{
		subnetID: sov.SubnetID,
		nodeID:   sov.NodeID,
	}
	isDeleted := sov.Weight == 0
	d.modifiedHasNodeIDs[subnetIDNodeID] = !isDeleted
	if isDeleted || sov.EndAccumulatedFee == 0 {
		// Validator is being deleted or is inactive
		return nil
	}
	d.active.ReplaceOrInsert(sov)
	return nil
}
