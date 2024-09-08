// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
)

var _ btree.LessFunc[*SubnetOnlyValidator] = (*SubnetOnlyValidator).Less

type SubnetOnlyValidator struct {
	// ValidationID is not serialized because it is used as the key in the
	// database, so it doesn't need to be stored in the value.
	ValidationID ids.ID

	SubnetID ids.ID     `serialize:"true"`
	NodeID   ids.NodeID `serialize:"true"`

	// PublicKey is the uncompressed BLS public key of the validator. It is
	// guaranteed to be populated.
	PublicKey []byte `serialize:"true"`

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
	EndAccumulatedFee uint64 `serialize:"true"`
}

// Less determines a canonical ordering of *SubnetOnlyValidators based on their
// EndAccumulatedFees and ValidationIDs.
//
// Returns true if:
//
//  1. This validator has a lower EndAccumulatedFee than the other.
//  2. This validator has an equal EndAccumulatedFee to the other and has a
//     lexicographically lower ValidationID.
func (v *SubnetOnlyValidator) Less(o *SubnetOnlyValidator) bool {
	switch {
	case v.EndAccumulatedFee < o.EndAccumulatedFee:
		return true
	case o.EndAccumulatedFee < v.EndAccumulatedFee:
		return false
	default:
		return v.ValidationID.Compare(o.ValidationID) == -1
	}
}

func getSubnetOnlyValidator(db database.KeyValueReader, validationID ids.ID) (*SubnetOnlyValidator, error) {
	bytes, err := db.Get(validationID[:])
	if err != nil {
		return nil, err
	}

	vdr := &SubnetOnlyValidator{
		ValidationID: validationID,
	}
	if _, err = block.GenesisCodec.Unmarshal(bytes, vdr); err != nil {
		return nil, fmt.Errorf("failed to unmarshal SubnetOnlyValidator: %w", err)
	}
	return vdr, err
}

func putSubnetOnlyValidator(db database.KeyValueWriter, vdr *SubnetOnlyValidator) error {
	bytes, err := block.GenesisCodec.Marshal(block.CodecVersion, vdr)
	if err != nil {
		return fmt.Errorf("failed to marshal SubnetOnlyValidator: %w", err)
	}
	return db.Put(vdr.ValidationID[:], bytes)
}

func deleteSubnetOnlyValidator(db database.KeyValueDeleter, validationID ids.ID) error {
	return db.Delete(validationID[:])
}
