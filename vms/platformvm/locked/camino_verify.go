// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package locked

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
)

var (
	errWrongInType  = errors.New("wrong input type")
	errWrongOutType = errors.New("wrong output type")
)

// Verifies that [ins] and [outs] have allowed types depending on [lockModeBonding].
// If lockModeBonding is true, than ins and outs can't be stakeable types.
// If lockModeBonding is false, than ins and outs can't be locked types.
func VerifyLockMode(
	ins []*avax.TransferableInput,
	outs []*avax.TransferableOutput,
	lockModeDepositBond bool,
) error {
	if lockModeDepositBond {
		for _, input := range ins {
			in := input.In

			if outerIn, ok := in.(*In); ok {
				in = outerIn.TransferableIn
			}

			if _, ok := in.(*stakeable.LockIn); ok {
				return errWrongInType
			}
		}

		for _, output := range outs {
			out := output.Out

			if outerOut, ok := out.(*Out); ok {
				out = outerOut.TransferableOut
			}

			if _, ok := out.(*stakeable.LockOut); ok {
				return errWrongOutType
			}
		}

		return nil
	}

	for _, input := range ins {
		in := input.In

		if outerIn, ok := in.(*stakeable.LockIn); ok {
			in = outerIn.TransferableIn
		}

		if _, ok := in.(*In); ok {
			return errWrongInType
		}
	}

	for _, output := range outs {
		out := output.Out

		if outerOut, ok := out.(*stakeable.LockOut); ok {
			out = outerOut.TransferableOut
		}

		if _, ok := out.(*Out); ok {
			return errWrongOutType
		}
	}

	return nil
}

// Verifies that [ins] and [outs] aren't stakeable or locked types.
func VerifyNoLocks(
	ins []*avax.TransferableInput,
	outs []*avax.TransferableOutput,
) error {
	for _, input := range ins {
		if _, ok := input.In.(*In); ok {
			return errWrongInType
		}
		if _, ok := input.In.(*stakeable.LockIn); ok {
			return errWrongInType
		}
	}

	for _, output := range outs {
		if _, ok := output.Out.(*Out); ok {
			return errWrongOutType
		}
		if _, ok := output.Out.(*stakeable.LockOut); ok {
			return errWrongOutType
		}
	}

	return nil
}
