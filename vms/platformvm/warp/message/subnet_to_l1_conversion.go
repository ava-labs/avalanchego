// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/types"
)

type SubnetToL1ConverstionValidatorData struct {
	NodeID       types.JSONByteSlice    `serialize:"true" json:"nodeID"`
	BLSPublicKey [bls.PublicKeyLen]byte `serialize:"true" json:"blsPublicKey"`
	Weight       uint64                 `serialize:"true" json:"weight"`
}

type SubnetToL1ConversionData struct {
	SubnetID       ids.ID                               `serialize:"true" json:"subnetID"`
	ManagerChainID ids.ID                               `serialize:"true" json:"managerChainID"`
	ManagerAddress types.JSONByteSlice                  `serialize:"true" json:"managerAddress"`
	Validators     []SubnetToL1ConverstionValidatorData `serialize:"true" json:"validators"`
}

// SubnetToL1ConversionID creates a subnet conversion ID from the provided
// subnet conversion data.
func SubnetToL1ConversionID(data SubnetToL1ConversionData) (ids.ID, error) {
	bytes, err := Codec.Marshal(CodecVersion, &data)
	if err != nil {
		return ids.Empty, err
	}
	return hashing.ComputeHash256Array(bytes), nil
}

// SubnetToL1Conversion reports the summary of the subnet conversation that
// occurred on the P-chain.
type SubnetToL1Conversion struct {
	payload

	// ID of the subnet conversion. It is typically generated by calling
	// SubnetToL1ConversionID.
	ID ids.ID `serialize:"true" json:"id"`
}

// NewSubnetToL1Conversion creates a new initialized SubnetToL1Conversion.
func NewSubnetToL1Conversion(id ids.ID) (*SubnetToL1Conversion, error) {
	msg := &SubnetToL1Conversion{
		ID: id,
	}
	return msg, Initialize(msg)
}

// ParseSubnetToL1Conversion parses bytes into an initialized SubnetToL1Conversion.
func ParseSubnetToL1Conversion(b []byte) (*SubnetToL1Conversion, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*SubnetToL1Conversion)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}
