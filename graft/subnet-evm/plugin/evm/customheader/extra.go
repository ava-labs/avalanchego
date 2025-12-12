// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/upgrade/subnetevm"
)

const (
	maximumExtraDataSize = 64 // Maximum size extra data may be after Genesis.
)

var (
	errInvalidExtraPrefix = errors.New("invalid header.Extra prefix")
	errInvalidExtraLength = errors.New("invalid header.Extra length")
)

// ExtraPrefix takes the previous header and the timestamp of its child
// block and calculates the expected extra prefix for the child block.
func ExtraPrefix(
	config *extras.ChainConfig,
	parent *types.Header,
	header *types.Header,
) ([]byte, error) {
	switch {
	case config.IsSubnetEVM(header.Time):
		window, err := feeWindow(config, parent, header.Time)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate fee window: %w", err)
		}
		return window.Bytes(), nil
	default:
		// Prior to SubnetEVM there was no expected extra prefix.
		return nil, nil
	}
}

// VerifyExtraPrefix verifies that the header's Extra field is correctly
// formatted.
func VerifyExtraPrefix(
	config *extras.ChainConfig,
	parent *types.Header,
	header *types.Header,
) error {
	if !config.IsSubnetEVM(header.Time) {
		return nil
	}

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
	return nil
}

// VerifyExtra verifies that the header's Extra field is correctly formatted for
// rules.
//
// TODO: Should this be merged with VerifyExtraPrefix?
func VerifyExtra(rules extras.AvalancheRules, extra []byte) error {
	extraLen := len(extra)
	switch {
	case rules.IsDurango:
		if extraLen < subnetevm.WindowSize {
			return fmt.Errorf(
				"%w: expected >= %d but got %d",
				errInvalidExtraLength,
				subnetevm.WindowSize,
				extraLen,
			)
		}
	case rules.IsSubnetEVM:
		if extraLen != subnetevm.WindowSize {
			return fmt.Errorf(
				"%w: expected %d but got %d",
				errInvalidExtraLength,
				subnetevm.WindowSize,
				extraLen,
			)
		}
	default:
		if uint64(extraLen) > maximumExtraDataSize {
			return fmt.Errorf(
				"%w: expected <= %d but got %d",
				errInvalidExtraLength,
				maximumExtraDataSize,
				extraLen,
			)
		}
	}
	return nil
}

// PredicateBytesFromExtra returns the predicate result bytes from the header's
// extra data. If the extra data is not long enough, an empty slice is returned.
func PredicateBytesFromExtra(extra []byte) []byte {
	offset := subnetevm.WindowSize
	// Prior to Durango, the VM enforces the extra data is smaller than or equal
	// to `offset`.
	// After Durango, the VM pre-verifies the extra data past `offset` is valid.
	if len(extra) <= offset {
		return nil
	}
	return extra[offset:]
}

// SetPredicateBytesInExtra sets the predicate result bytes in the header's extra
// data. If the extra data is not long enough (i.e., an incomplete header.Extra
// as built in the miner), it is padded with zeros.
func SetPredicateBytesInExtra(extra []byte, predicateBytes []byte) []byte {
	offset := subnetevm.WindowSize
	if len(extra) < offset {
		// pad extra with zeros
		extra = append(extra, make([]byte, offset-len(extra))...)
	} else {
		// truncate extra to the offset
		extra = extra[:offset]
	}
	extra = append(extra, predicateBytes...)
	return extra
}
