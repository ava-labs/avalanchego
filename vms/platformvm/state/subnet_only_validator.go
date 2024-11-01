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
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
)

var (
	_ btree.LessFunc[SubnetOnlyValidator] = SubnetOnlyValidator.Less
	_ utils.Sortable[SubnetOnlyValidator] = SubnetOnlyValidator{}

	ErrMutatedSubnetOnlyValidator = errors.New("subnet only validator contains mutated constant fields")
)

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
