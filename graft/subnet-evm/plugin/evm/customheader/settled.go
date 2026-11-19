// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
)

var errSAEFieldSet = errors.New("SAE header field set")

// VerifyNoSAEFields returns an error if header sets a field that only SAE
// blocks use.
func VerifyNoSAEFields(header *types.Header) error {
	extra := customtypes.GetHeaderExtra(header)
	if extra.SettledHeight != nil ||
		extra.SettledGasUnix != nil ||
		extra.SettledGasNumerator != nil ||
		extra.SettledExcess != nil ||
		extra.TargetExponent != nil ||
		extra.TargetToExcessScaling != nil ||
		extra.MinPrice != nil {
		return fmt.Errorf("%w: %s", errSAEFieldSet, header.Hash())
	}
	return nil
}
