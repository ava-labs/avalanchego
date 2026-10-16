// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
)

// Sizes of the fee state that prefixes the extra data of pre-Helicon headers.
const (
	ap3WindowSize   = 10 * wrappers.LongLen
	acp176StateSize = 3 * wrappers.LongLen // capacity, excess, target excess
)

var ErrInsufficientFeeState = errors.New("insufficient length for ACP-176 fee state")

// TargetExponentFromExtra returns the ACP-176 target excess from a Fortuna or
// Granite header's extra data.
func TargetExponentFromExtra(extra []byte) (dynamic.TargetExponent, error) {
	if len(extra) < acp176StateSize {
		return 0, fmt.Errorf("%w: expected at least %d bytes but got %d bytes",
			ErrInsufficientFeeState,
			acp176StateSize,
			len(extra),
		)
	}
	return dynamic.TargetExponent(binary.BigEndian.Uint64(extra[2*wrappers.LongLen:])), nil
}

// PredicateBytesFromExtra returns the predicate result bytes from the header's
// extra data. If the extra data is not long enough, an empty slice is returned.
func PredicateBytesFromExtra(rules extras.AvalancheRules, extra []byte) []byte {
	offset := predicateBytesOffset(rules)
	// Prior to Durango, the VM enforces the extra data is smaller than or equal
	// to `offset`.
	// After Durango, the VM pre-verifies the extra data past `offset` is valid.
	if len(extra) <= offset {
		return nil
	}
	return extra[offset:]
}

func predicateBytesOffset(rules extras.AvalancheRules) int {
	switch {
	case rules.IsHelicon:
		return 0
	case rules.IsFortuna:
		return acp176StateSize
	default:
		return ap3WindowSize
	}
}
