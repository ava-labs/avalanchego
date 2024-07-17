// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func MeterInput(c codec.Manager, v uint16, in *avax.TransferableInput) (Dimensions, error) {
	cost, err := in.In.Cost()
	if err != nil {
		return Dimensions{}, fmt.Errorf("failed retrieving cost of input %s: %w", in.ID, err)
	}

	inSize, err := c.Size(v, in)
	if err != nil {
		return Dimensions{}, fmt.Errorf("failed retrieving size of input %s: %w", in.ID, err)
	}
	uInSize := uint64(inSize)

	return Dimensions{
		Bandwidth: uInSize - codec.VersionSize,
		DBRead:    uInSize, // inputs are read
		DBWrite:   uInSize, // inputs are deleted
		Compute:   cost,
	}, nil
}

func MeterOutput(c codec.Manager, v uint16, out *avax.TransferableOutput) (Dimensions, error) {
	outSize, err := c.Size(v, out)
	if err != nil {
		return Dimensions{}, fmt.Errorf("failed retrieving size of output %s: %w", out.ID, err)
	}
	uOutSize := uint64(outSize)

	return Dimensions{
		Bandwidth: uOutSize - codec.VersionSize,
		DBWrite:   uOutSize,
	}, nil
}

func MeterCredential(c codec.Manager, v uint16, keysCount int) (Dimensions, error) {
	// Ensure that codec picks interface instead of the pointer to evaluate size.
	var cred verify.Verifiable = &secp256k1fx.Credential{
		Sigs: make([][secp256k1.SignatureLen]byte, keysCount),
	}
	credSize, err := c.Size(v, &cred)
	if err != nil {
		return Dimensions{}, fmt.Errorf("failed retrieving size of credentials: %w", err)
	}

	return Dimensions{
		Bandwidth: uint64(credSize) - codec.VersionSize,
	}, nil
}
