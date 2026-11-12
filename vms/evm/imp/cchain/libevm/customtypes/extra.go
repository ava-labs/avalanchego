// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/graft/evm/predicate"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

// An ExtraFormat is a layout of a header's Extra field.
type ExtraFormat uint8

const (
	// AP3ExtraFormat prefixes the predicate results with the Apricot Phase 3
	// fee window.
	AP3ExtraFormat ExtraFormat = iota
	// FortunaExtraFormat prefixes the predicate results with the ACP-176 fee
	// state.
	FortunaExtraFormat
	// HeliconExtraFormat holds only the predicate results.
	HeliconExtraFormat
)

// Sizes of the fee state that prefixes the extra data of pre-Helicon headers.
const (
	ap3WindowSize   = 10 * wrappers.LongLen
	acp176StateSize = 3 * wrappers.LongLen // capacity, excess, target excess
)

var ErrInsufficientFeeState = errors.New("insufficient length for ACP-176 fee state")

// PredicateResults returns the predicate results stored in h.Extra, caching
// them on h. If there are none, or they cannot be parsed, nil is returned. The
// format MUST be the one in effect at h's timestamp.
func PredicateResults(h *ethtypes.Header, format ExtraFormat) predicate.BlockResults {
	predicateBytes := predicateBytesFromExtra(format, h.Extra)
	if len(predicateBytes) == 0 {
		return nil
	}

	cache := &GetHeaderExtra(h).predicateResults
	if results := cache.Load(); results != nil {
		return *results
	}

	// results were already verified for canonical execution
	results, _ := predicate.ParseBlockResults(predicateBytes)
	cache.Store(&results)
	return results
}

// predicateBytesFromExtra returns the predicate result bytes from the header's
// extra data. If the extra data is not long enough, an empty slice is returned.
func predicateBytesFromExtra(format ExtraFormat, extra []byte) []byte {
	offset := ap3WindowSize
	switch format {
	case HeliconExtraFormat:
		offset = 0
	case FortunaExtraFormat:
		offset = acp176StateSize
	}
	// Prior to Durango, the VM enforces the extra data is smaller than or equal
	// to `offset`.
	// After Durango, the VM pre-verifies the extra data past `offset` is valid.
	if len(extra) <= offset {
		return nil
	}
	return extra[offset:]
}

// TargetExponentFromExtra returns the target exponent stored as the last field
// of the ACP-176 fee state prefixing the extra data of Fortuna through
// pre-Helicon headers.
func TargetExponentFromExtra(extra []byte) (dynamic.TargetExponent, error) {
	if len(extra) < acp176StateSize {
		return 0, fmt.Errorf("%w: expected at least %d bytes but got %d bytes",
			ErrInsufficientFeeState,
			acp176StateSize,
			len(extra),
		)
	}
	const targetIndex = acp176StateSize - wrappers.LongLen
	return dynamic.TargetExponent(binary.BigEndian.Uint64(extra[targetIndex:])), nil
}
