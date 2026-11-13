// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
)

// errSAEFieldSet is returned when a subnet-evm block carries an SAE header
// field. These fields belong to SAE and must not appear on subnet-evm blocks.
var errSAEFieldSet = errors.New("remote SAE header field should be nil")

// VerifyNoSAEFields rejects any subnet-evm block carrying an SAE header field:
// the settled block marker, the target excess, or the gas config. These fields
// belong to SAE. semanticVerify only runs on subnet-evm's own block.Verify, so
// subnet-evm never verifies an SAE block and these fields must always be
// absent.
func VerifyNoSAEFields(header *types.Header) error {
	extra := customtypes.GetHeaderExtra(header)
	if extra.SettledHeight != nil ||
		extra.SettledGasUnix != nil ||
		extra.SettledGasNumerator != nil ||
		extra.SettledExcess != nil ||
		extra.TargetExcess != nil ||
		extra.GasConfigTargetToExcessScaling != nil ||
		extra.GasConfigMinGasPrice != nil ||
		extra.GasConfigStaticPricing != nil {
		return fmt.Errorf("%w: %s", errSAEFieldSet, header.Hash())
	}
	return nil
}
