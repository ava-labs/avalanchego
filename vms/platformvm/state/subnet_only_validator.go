// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
)

var (
	_ btree.LessFunc[SubnetOnlyValidator] = SubnetOnlyValidator.Less
	_ utils.Sortable[SubnetOnlyValidator] = SubnetOnlyValidator{}

	ErrMutatedSubnetOnlyValidator     = errors.New("subnet-only validator contains mutated constant fields")
	ErrConflictingSubnetOnlyValidator = errors.New("subnet-only validator contains conflicting subnetID + nodeID pair")
	ErrDuplicateSubnetOnlyValidator   = errors.New("subnet-only validator contains duplicate subnetID + nodeID pair")
)

type SubnetOnlyValidators interface {
	// GetActiveSubnetOnlyValidatorsIterator returns an iterator of all the
	// active subnet-only validators in increasing order of EndAccumulatedFee.
	GetActiveSubnetOnlyValidatorsIterator() (iterator.Iterator[SubnetOnlyValidator], error)

	// NumActiveSubnetOnlyValidators returns the number of currently active
	// subnet-only validators.
	NumActiveSubnetOnlyValidators() int

	// WeightOfSubnetOnlyValidators returns the total active and inactive weight
	// of subnet-only validators on [subnetID].
	WeightOfSubnetOnlyValidators(subnetID ids.ID) (uint64, error)

	// GetSubnetOnlyValidator returns the validator with [validationID] if it
	// exists. If the validator does not exist, [err] will equal
	// [database.ErrNotFound].
	GetSubnetOnlyValidator(validationID ids.ID) (SubnetOnlyValidator, error)

	// HasSubnetOnlyValidator returns the validator with [validationID] if it
	// exists.
	HasSubnetOnlyValidator(subnetID ids.ID, nodeID ids.NodeID) (bool, error)

	// PutSubnetOnlyValidator inserts [sov] as a validator. If the weight of the
	// validator is 0, the validator is removed.
	//
	// If inserting this validator attempts to modify any of the constant fields
	// of the subnet-only validator struct, an error will be returned.
	//
	// If inserting this validator would cause the total weight of subnet-only
	// validators on a subnet to overflow MaxUint64, an error will be returned.
	//
	// If inserting this validator would cause there to be multiple validators
	// with the same subnetID and nodeID pair to exist at the same time, an
	// error will be returned.
	//
	// If an SoV with the same validationID as a previously removed SoV is
	// added, the behavior is undefined.
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

// Compare determines a canonical ordering of SubnetOnlyValidators based on
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

// immutableFieldsAreUnmodified returns true if two versions of the same
// validator are valid. Either because the validationID has changed or because
// no unexpected fields have been modified.
func (v SubnetOnlyValidator) immutableFieldsAreUnmodified(o SubnetOnlyValidator) bool {
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

func (v SubnetOnlyValidator) isDeleted() bool {
	return v.Weight == 0
}

func (v SubnetOnlyValidator) isActive() bool {
	return v.Weight != 0 && v.EndAccumulatedFee != 0
}

func (v SubnetOnlyValidator) effectiveValidationID() ids.ID {
	if v.isActive() {
		return v.ValidationID
	}
	return ids.Empty
}

func (v SubnetOnlyValidator) effectiveNodeID() ids.NodeID {
	if v.isActive() {
		return v.NodeID
	}
	return ids.EmptyNodeID
}

func (v SubnetOnlyValidator) effectivePublicKey() *bls.PublicKey {
	if v.isActive() {
		return bls.PublicKeyFromValidUncompressedBytes(v.PublicKey)
	}
	return nil
}

func (v SubnetOnlyValidator) effectivePublicKeyBytes() []byte {
	if v.isActive() {
		return v.PublicKey
	}
	return nil
}

func getSubnetOnlyValidator(
	cache cache.Cacher[ids.ID, maybe.Maybe[SubnetOnlyValidator]],
	db database.KeyValueReader,
	validationID ids.ID,
) (SubnetOnlyValidator, error) {
	if maybeSOV, ok := cache.Get(validationID); ok {
		if maybeSOV.IsNothing() {
			return SubnetOnlyValidator{}, database.ErrNotFound
		}
		return maybeSOV.Value(), nil
	}

	bytes, err := db.Get(validationID[:])
	if err == database.ErrNotFound {
		cache.Put(validationID, maybe.Nothing[SubnetOnlyValidator]())
		return SubnetOnlyValidator{}, database.ErrNotFound
	}
	if err != nil {
		return SubnetOnlyValidator{}, err
	}

	sov := SubnetOnlyValidator{
		ValidationID: validationID,
	}
	if _, err := block.GenesisCodec.Unmarshal(bytes, &sov); err != nil {
		return SubnetOnlyValidator{}, fmt.Errorf("failed to unmarshal SubnetOnlyValidator: %w", err)
	}

	cache.Put(validationID, maybe.Some(sov))
	return sov, nil
}

func putSubnetOnlyValidator(
	db database.KeyValueWriter,
	cache cache.Cacher[ids.ID, maybe.Maybe[SubnetOnlyValidator]],
	sov SubnetOnlyValidator,
) error {
	bytes, err := block.GenesisCodec.Marshal(block.CodecVersion, sov)
	if err != nil {
		return fmt.Errorf("failed to marshal SubnetOnlyValidator: %w", err)
	}
	if err := db.Put(sov.ValidationID[:], bytes); err != nil {
		return err
	}

	cache.Put(sov.ValidationID, maybe.Some(sov))
	return nil
}

func deleteSubnetOnlyValidator(
	db database.KeyValueDeleter,
	cache cache.Cacher[ids.ID, maybe.Maybe[SubnetOnlyValidator]],
	validationID ids.ID,
) error {
	if err := db.Delete(validationID[:]); err != nil {
		return err
	}

	cache.Put(validationID, maybe.Nothing[SubnetOnlyValidator]())
	return nil
}

type subnetOnlyValidatorsDiff struct {
	netAddedActive      int               // May be negative
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

// getActiveSubnetOnlyValidatorsIterator takes in the parent iterator, removes
// all modified validators, and then adds all modified active validators.
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

func (d *subnetOnlyValidatorsDiff) putSubnetOnlyValidator(state Chain, sov SubnetOnlyValidator) error {
	var (
		prevWeight uint64
		prevActive bool
		newActive  = sov.isActive()
	)
	switch priorSOV, err := state.GetSubnetOnlyValidator(sov.ValidationID); err {
	case nil:
		if !priorSOV.immutableFieldsAreUnmodified(sov) {
			return ErrMutatedSubnetOnlyValidator
		}

		prevWeight = priorSOV.Weight
		prevActive = priorSOV.isActive()
	case database.ErrNotFound:
		// Verify that there is not a legacy subnet validator with the same
		// subnetID+nodeID as this L1 validator.
		_, err := state.GetCurrentValidator(sov.SubnetID, sov.NodeID)
		if err == nil {
			return ErrConflictingSubnetOnlyValidator
		}
		if err != database.ErrNotFound {
			return err
		}

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

	if prevWeight != sov.Weight {
		weight, err := state.WeightOfSubnetOnlyValidators(sov.SubnetID)
		if err != nil {
			return err
		}

		weight, err = math.Sub(weight, prevWeight)
		if err != nil {
			return err
		}
		weight, err = math.Add(weight, sov.Weight)
		if err != nil {
			return err
		}

		d.modifiedTotalWeight[sov.SubnetID] = weight
	}

	switch {
	case prevActive && !newActive:
		d.netAddedActive--
	case !prevActive && newActive:
		d.netAddedActive++
	}

	if prevSOV, ok := d.modified[sov.ValidationID]; ok {
		d.active.Delete(prevSOV)
	}
	d.modified[sov.ValidationID] = sov

	subnetIDNodeID := subnetIDNodeID{
		subnetID: sov.SubnetID,
		nodeID:   sov.NodeID,
	}
	d.modifiedHasNodeIDs[subnetIDNodeID] = !sov.isDeleted()
	if sov.isActive() {
		d.active.ReplaceOrInsert(sov)
	}
	return nil
}

type activeSubnetOnlyValidators struct {
	lookup map[ids.ID]SubnetOnlyValidator
	tree   *btree.BTreeG[SubnetOnlyValidator]
}

func newActiveSubnetOnlyValidators() *activeSubnetOnlyValidators {
	return &activeSubnetOnlyValidators{
		lookup: make(map[ids.ID]SubnetOnlyValidator),
		tree:   btree.NewG(defaultTreeDegree, SubnetOnlyValidator.Less),
	}
}

func (a *activeSubnetOnlyValidators) get(validationID ids.ID) (SubnetOnlyValidator, bool) {
	sov, ok := a.lookup[validationID]
	return sov, ok
}

func (a *activeSubnetOnlyValidators) put(sov SubnetOnlyValidator) {
	a.lookup[sov.ValidationID] = sov
	a.tree.ReplaceOrInsert(sov)
}

func (a *activeSubnetOnlyValidators) delete(validationID ids.ID) bool {
	sov, ok := a.lookup[validationID]
	if !ok {
		return false
	}

	delete(a.lookup, validationID)
	a.tree.Delete(sov)
	return true
}

func (a *activeSubnetOnlyValidators) len() int {
	return len(a.lookup)
}

func (a *activeSubnetOnlyValidators) newIterator() iterator.Iterator[SubnetOnlyValidator] {
	return iterator.FromTree(a.tree)
}

func (a *activeSubnetOnlyValidators) addStakersToValidatorManager(vdrs validators.Manager) error {
	for validationID, sov := range a.lookup {
		pk := bls.PublicKeyFromValidUncompressedBytes(sov.PublicKey)
		if err := vdrs.AddStaker(sov.SubnetID, sov.NodeID, pk, validationID, sov.Weight); err != nil {
			return err
		}
	}
	return nil
}

func addSoVToValidatorManager(vdrs validators.Manager, sov SubnetOnlyValidator) error {
	nodeID := sov.effectiveNodeID()
	if vdrs.GetWeight(sov.SubnetID, nodeID) != 0 {
		return vdrs.AddWeight(sov.SubnetID, nodeID, sov.Weight)
	}
	return vdrs.AddStaker(
		sov.SubnetID,
		nodeID,
		sov.effectivePublicKey(),
		sov.effectiveValidationID(),
		sov.Weight,
	)
}
