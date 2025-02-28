// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/acp176"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap3"
)

var (
	errInvalidExtraPrefix = errors.New("invalid header.Extra prefix")
	errIncorrectFeeState  = errors.New("incorrect fee state")
	errInvalidExtraLength = errors.New("invalid header.Extra length")
)

// ExtraPrefix returns what the prefix of the header's Extra field should be
// based on the desired target excess.
//
// If the `desiredTargetExcess` is nil, the parent's target excess is used.
func ExtraPrefix(
	config *params.ChainConfig,
	parent *types.Header,
	header *types.Header,
	desiredTargetExcess *gas.Gas,
) ([]byte, error) {
	switch {
	case config.IsFUpgrade(header.Time):
		state, err := feeStateAfterBlock(
			config,
			parent,
			header,
			desiredTargetExcess,
		)
		if err != nil {
			return nil, fmt.Errorf("calculating fee state: %w", err)
		}
		return state.Bytes(), nil
	case config.IsApricotPhase3(header.Time):
		window, err := feeWindow(config, parent, header.Time)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate fee window: %w", err)
		}
		return window.Bytes(), nil
	default:
		// Prior to AP3 there was no expected extra prefix.
		return nil, nil
	}
}

// VerifyExtraPrefix verifies that the header's Extra field is correctly
// formatted.
func VerifyExtraPrefix(
	config *params.ChainConfig,
	parent *types.Header,
	header *types.Header,
) error {
	switch {
	case config.IsFUpgrade(header.Time):
		remoteState, err := acp176.ParseState(header.Extra)
		if err != nil {
			return fmt.Errorf("parsing remote fee state: %w", err)
		}

		// By passing in the claimed target excess, we ensure that the expected
		// target excess is equal to the claimed target excess if it is possible
		// to have correctly set it to that value. Otherwise, the resulting
		// value will be as close to the claimed value as possible, but would
		// not be equal.
		expectedState, err := feeStateAfterBlock(
			config,
			parent,
			header,
			&remoteState.TargetExcess,
		)
		if err != nil {
			return fmt.Errorf("calculating expected fee state: %w", err)
		}

		if remoteState != expectedState {
			return fmt.Errorf("%w: expected %+v, found %+v",
				errIncorrectFeeState,
				expectedState,
				remoteState,
			)
		}
	case config.IsApricotPhase3(header.Time):
		feeWindow, err := feeWindow(config, parent, header.Time)
		if err != nil {
			return fmt.Errorf("calculating expected fee window: %w", err)
		}
		feeWindowBytes := feeWindow.Bytes()
		if !bytes.HasPrefix(header.Extra, feeWindowBytes) {
			return fmt.Errorf("%w: expected %x as prefix, found %x",
				errInvalidExtraPrefix,
				feeWindowBytes,
				header.Extra,
			)
		}
	}
	return nil
}

// VerifyExtra verifies that the header's Extra field is correctly formatted for
// rules.
//
// TODO: Should this be merged with VerifyExtraPrefix?
func VerifyExtra(rules params.AvalancheRules, extra []byte) error {
	extraLen := len(extra)
	switch {
	case rules.IsFUpgrade:
		if extraLen < acp176.StateSize {
			return fmt.Errorf(
				"%w: expected >= %d but got %d",
				errInvalidExtraLength,
				acp176.StateSize,
				extraLen,
			)
		}
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
func PredicateBytesFromExtra(rules params.AvalancheRules, extra []byte) []byte {
	offset := ap3.WindowSize
	if rules.IsFUpgrade {
		offset = acp176.StateSize
	}

	// Prior to Durango, the VM enforces the extra data is smaller than or equal
	// to `offset`.
	// After Durango, the VM pre-verifies the extra data past `offset` is valid.
	if len(extra) <= offset {
		return nil
	}
	return extra[offset:]
}
