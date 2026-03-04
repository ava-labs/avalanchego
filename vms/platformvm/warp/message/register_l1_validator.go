// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/types"
)

var (
	ErrInvalidSubnetID = errors.New("invalid subnet ID")
	ErrInvalidWeight   = errors.New("invalid weight")
	ErrInvalidNodeID   = errors.New("invalid node ID")
	ErrInvalidOwner    = errors.New("invalid owner")
)

type PChainOwner struct {
	// The threshold number of `Addresses` that must provide a signature in
	// order for the `PChainOwner` to be considered valid.
	Threshold uint32 `serialize:"true" json:"threshold"`
	// The addresses that are allowed to sign to authenticate a `PChainOwner`.
	Addresses []ids.ShortID `serialize:"true" json:"addresses"`
}

// RegisterL1Validator adds a validator to the subnet.
type RegisterL1Validator struct {
	payload

	SubnetID              ids.ID                 `serialize:"true" json:"subnetID"`
	NodeID                types.JSONByteSlice    `serialize:"true" json:"nodeID"`
	BLSPublicKey          [bls.PublicKeyLen]byte `serialize:"true" json:"blsPublicKey"`
	Expiry                uint64                 `serialize:"true" json:"expiry"`
	RemainingBalanceOwner PChainOwner            `serialize:"true" json:"remainingBalanceOwner"`
	DisableOwner          PChainOwner            `serialize:"true" json:"disableOwner"`
	Weight                uint64                 `serialize:"true" json:"weight"`
}

func (r *RegisterL1Validator) Verify() error {
	if r.SubnetID == constants.PrimaryNetworkID {
		return ErrInvalidSubnetID
	}
	if r.Weight == 0 {
		return ErrInvalidWeight
	}

	nodeID, err := ids.ToNodeID(r.NodeID)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidNodeID, err)
	}
	if nodeID == ids.EmptyNodeID {
		return fmt.Errorf("%w: empty nodeID is disallowed", ErrInvalidNodeID)
	}

	err = verify.All(
		&secp256k1fx.OutputOwners{
			Threshold: r.RemainingBalanceOwner.Threshold,
			Addrs:     r.RemainingBalanceOwner.Addresses,
		},
		&secp256k1fx.OutputOwners{
			Threshold: r.DisableOwner.Threshold,
			Addrs:     r.DisableOwner.Addresses,
		},
	)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidOwner, err)
	}
	return nil
}

func (r *RegisterL1Validator) ValidationID() ids.ID {
	return hashing.ComputeHash256Array(r.Bytes())
}

// NewRegisterL1Validator creates a new initialized RegisterL1Validator.
func NewRegisterL1Validator(
	subnetID ids.ID,
	nodeID ids.NodeID,
	blsPublicKey [bls.PublicKeyLen]byte,
	expiry uint64,
	remainingBalanceOwner PChainOwner,
	disableOwner PChainOwner,
	weight uint64,
) (*RegisterL1Validator, error) {
	msg := &RegisterL1Validator{
		SubnetID:              subnetID,
		NodeID:                nodeID[:],
		BLSPublicKey:          blsPublicKey,
		Expiry:                expiry,
		RemainingBalanceOwner: remainingBalanceOwner,
		DisableOwner:          disableOwner,
		Weight:                weight,
	}
	return msg, Initialize(msg)
}

// ParseRegisterL1Validator parses bytes into an initialized
// RegisterL1Validator.
func ParseRegisterL1Validator(b []byte) (*RegisterL1Validator, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*RegisterL1Validator)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}
