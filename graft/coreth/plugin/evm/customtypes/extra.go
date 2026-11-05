// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/evm/warpmsg"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cparams"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

// Sizes of the fee state that prefixes the extra data of pre-Helicon headers.
const (
	ap3WindowSize   = 10 * wrappers.LongLen
	acp176StateSize = 3 * wrappers.LongLen // capacity, excess, target excess
)

// PredicateResults returns the parsed predicate results from the header's extra
// data. If the extra data does not contain predicate results, or cannot be
// parsed, nil is returned. The same rules MUST be supplied for each call to a
// given header.
func PredicateResults(h *ethtypes.Header, rules cparams.Rules) warpmsg.BlockResults {
	predicateBytes := PredicateBytesFromExtra(rules, h.Extra)
	if len(predicateBytes) == 0 {
		return nil
	}

	cache := &GetHeaderExtra(h).predicateResults
	if results := cache.Load(); results != nil {
		return *results
	}

	// results were already verified for canonical execution
	results, _ := warpmsg.ParseBlockResults(predicateBytes)
	cache.Store(&results)
	return results
}

var ErrInsufficientFeeState = errors.New("insufficient length for ACP-176 fee state")

// PredicateBytesFromExtra returns the predicate result bytes from the header's
// extra data. If the extra data is not long enough, an empty slice is returned.
func PredicateBytesFromExtra(rules cparams.Rules, extra []byte) []byte {
	offset := predicateBytesOffset(rules)
	// Prior to Durango, the VM enforces the extra data is smaller than or equal
	// to `offset`.
	// After Durango, the VM pre-verifies the extra data past `offset` is valid.
	if len(extra) <= offset {
		return nil
	}
	return extra[offset:]
}

func predicateBytesOffset(rules cparams.Rules) int {
	switch {
	case rules.IsHelicon:
		return 0
	case rules.IsFortuna:
		return acp176StateSize
	default:
		return ap3WindowSize
	}
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
