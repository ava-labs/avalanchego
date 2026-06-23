// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
)

// ErrSettledMarkerSet is returned when a coreth block carries a settled block
// marker. The marker belongs to SAE and must not appear on coreth blocks.
var ErrSettledMarkerSet = errors.New("remote settled block marker should be nil")

// VerifySettled rejects any coreth block carrying a settled block marker.
// The Settled* fields belong to SAE. semanticVerify only runs on coreth's own
// block.Verify, so coreth never verifies an SAE block and these fields must
// always be absent.
func VerifySettled(header *types.Header) error {
	extra := customtypes.GetHeaderExtra(header)
	if extra.SettledHeight != nil ||
		extra.SettledGasUnix != nil ||
		extra.SettledGasNumerator != nil ||
		extra.SettledExcess != nil {
		return fmt.Errorf("%w: %s", ErrSettledMarkerSet, header.Hash())
	}
	return nil
}
