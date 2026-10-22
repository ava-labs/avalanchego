// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"encoding/binary"
	"errors"
	"fmt"

	paramsextras "github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// Sizes of the fee state that prefixes the extra data of pre-Helicon headers.
const (
	ap3WindowSize   = 10 * wrappers.LongLen
	ACP176StateSize = 3 * wrappers.LongLen // capacity, excess, target excess
)

var ErrInsufficientFeeState = errors.New("insufficient length for ACP-176 fee state")

// PredicateBytesFromExtra returns the predicate result bytes from the header's
// extra data. If the extra data is not long enough, an empty slice is returned.
func PredicateBytesFromExtra(rules paramsextras.AvalancheRules, extra []byte) []byte {
	offset := predicateBytesOffset(rules)
	// Prior to Durango, the VM enforces the extra data is smaller than or equal
	// to `offset`.
	// After Durango, the VM pre-verifies the extra data past `offset` is valid.
	if len(extra) <= offset {
		return nil
	}
	return extra[offset:]
}

func predicateBytesOffset(rules paramsextras.AvalancheRules) int {
	switch {
	case rules.IsHelicon:
		return 0
	case rules.IsFortuna:
		return ACP176StateSize
	default:
		return ap3WindowSize
	}
}

// TargetExcessFromExtra returns the target excess stored as the last field of
// the ACP-176 fee state prefixing the extra data of Fortuna through pre-Helicon
// headers.
func TargetExcessFromExtra(extra []byte) (uint64, error) {
	if len(extra) < ACP176StateSize {
		return 0, fmt.Errorf("%w: expected at least %d bytes but got %d bytes",
			ErrInsufficientFeeState,
			ACP176StateSize,
			len(extra),
		)
	}
	return binary.BigEndian.Uint64(extra[ACP176StateSize-wrappers.LongLen:]), nil
}
