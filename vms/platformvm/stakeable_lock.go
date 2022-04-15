// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"

	"github.com/chain4travel/caminogo/vms/components/avax"
)

var (
	errInvalidLocktime      = errors.New("invalid locktime")
	errNestedStakeableLocks = errors.New("shouldn't nest stakeable locks")
)

type StakeableLockOut struct {
	Locktime             uint64 `serialize:"true" json:"locktime"`
	avax.TransferableOut `serialize:"true" json:"output"`
}

func (s *StakeableLockOut) Addresses() [][]byte {
	if addressable, ok := s.TransferableOut.(avax.Addressable); ok {
		return addressable.Addresses()
	}
	return nil
}

func (s *StakeableLockOut) Verify() error {
	if s.Locktime == 0 {
		return errInvalidLocktime
	}
	if _, nested := s.TransferableOut.(*StakeableLockOut); nested {
		return errNestedStakeableLocks
	}
	return s.TransferableOut.Verify()
}

type StakeableLockIn struct {
	Locktime            uint64 `serialize:"true" json:"locktime"`
	avax.TransferableIn `serialize:"true" json:"input"`
}

func (s *StakeableLockIn) Verify() error {
	if s.Locktime == 0 {
		return errInvalidLocktime
	}
	if _, nested := s.TransferableIn.(*StakeableLockIn); nested {
		return errNestedStakeableLocks
	}
	return s.TransferableIn.Verify()
}
