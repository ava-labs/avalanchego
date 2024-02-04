// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func GetInputsDimensions(c codec.Manager, v uint16, ins []*avax.TransferableInput) (Dimensions, error) {
	var consumedUnits Dimensions
	for _, in := range ins {
		cost, err := in.In.Cost()
		if err != nil {
			return consumedUnits, fmt.Errorf("failed retrieving cost of input %s: %w", in.ID, err)
		}

		inSize, err := c.Size(v, in)
		if err != nil {
			return consumedUnits, fmt.Errorf("failed retrieving size of input %s: %w", in.ID, err)
		}
		uInSize := uint64(inSize)

		consumedUnits[Bandwidth] += uInSize - codec.CodecVersionSize
		consumedUnits[UTXORead] += cost + uInSize // inputs are read
		consumedUnits[UTXOWrite] += uInSize       // inputs are deleted
	}
	return consumedUnits, nil
}

func GetOutputsDimensions(c codec.Manager, v uint16, outs []*avax.TransferableOutput) (Dimensions, error) {
	var consumedUnits Dimensions
	for _, out := range outs {
		outSize, err := c.Size(v, out)
		if err != nil {
			return consumedUnits, fmt.Errorf("failed retrieving size of output %s: %w", out.ID, err)
		}
		uOutSize := uint64(outSize)

		consumedUnits[Bandwidth] += uOutSize - codec.CodecVersionSize
		consumedUnits[UTXOWrite] += uOutSize
	}
	return consumedUnits, nil
}

func GetCredentialsDimensions(c codec.Manager, v uint16, inputSigIndices []uint32) (Dimensions, error) {
	var consumedUnits Dimensions

	// Workaround to ensure that codec picks interface instead of the pointer to evaluate size.
	// TODO ABENEGIA: fix this
	creds := make([]verify.Verifiable, 0, 1)
	creds = append(creds, &secp256k1fx.Credential{
		Sigs: make([][secp256k1.SignatureLen]byte, len(inputSigIndices)),
	})

	credSize, err := c.Size(v, creds)
	if err != nil {
		return consumedUnits, fmt.Errorf("failed retrieving size of credentials: %w", err)
	}
	credSize -= wrappers.IntLen // length of the slice, we want the single credential

	consumedUnits[Bandwidth] += uint64(credSize) - codec.CodecVersionSize
	return consumedUnits, nil
}
