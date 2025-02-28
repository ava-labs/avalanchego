// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"errors"
	"fmt"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap3"
)

var errInvalidExtraLength = errors.New("invalid header.Extra length")

// ExtraPrefix takes the previous header and the timestamp of its child block
// and calculates the expected extra prefix for the child block.
func ExtraPrefix(
	config *params.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) ([]byte, error) {
	switch {
	case config.IsApricotPhase3(timestamp):
		window, err := feeWindow(config, parent, timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate fee window: %w", err)
		}
		return window.Bytes(), nil
	default:
		// Prior to AP3 there was no expected extra prefix.
		return nil, nil
	}
}

// VerifyExtra verifies that the header's Extra field is correctly formatted for
// [rules].
func VerifyExtra(rules params.AvalancheRules, extra []byte) error {
	extraLen := len(extra)
	switch {
	case rules.IsDurango:
		if extraLen < ap3.WindowSize {
			return fmt.Errorf(
				"%w: expected >= %d but got %d",
				errInvalidExtraLength,
				ap3.WindowSize,
				extraLen,
			)
		}
	case rules.IsApricotPhase3:
		if extraLen != ap3.WindowSize {
			return fmt.Errorf(
				"%w: expected %d but got %d",
				errInvalidExtraLength,
				ap3.WindowSize,
				extraLen,
			)
		}
	case rules.IsApricotPhase1:
		if extraLen != 0 {
			return fmt.Errorf(
				"%w: expected 0 but got %d",
				errInvalidExtraLength,
				extraLen,
			)
		}
	default:
		if uint64(extraLen) > params.MaximumExtraDataSize {
			return fmt.Errorf(
				"%w: expected <= %d but got %d",
				errInvalidExtraLength,
				params.MaximumExtraDataSize,
				extraLen,
			)
		}
	}
	return nil
}

// PredicateBytesFromExtra returns the predicate result bytes from the header's
// extra data. If the extra data is not long enough, an empty slice is returned.
func PredicateBytesFromExtra(_ params.AvalancheRules, extra []byte) []byte {
	// Prior to Durango, the VM enforces the extra data is smaller than or equal
	// to this size.
	// After Durango, the VM pre-verifies the extra data past the dynamic fee
	// rollup window is valid.
	if len(extra) <= ap3.WindowSize {
		return nil
	}
	return extra[ap3.WindowSize:]
}
