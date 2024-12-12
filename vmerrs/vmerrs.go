// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package vmerrs

import (
	"errors"

	"github.com/ethereum/go-ethereum/core/vm"
)

// List evm execution errors
var (
	ErrOutOfGas                    = vm.ErrOutOfGas
	ErrCodeStoreOutOfGas           = vm.ErrCodeStoreOutOfGas
	ErrDepth                       = vm.ErrDepth
	ErrInsufficientBalance         = vm.ErrInsufficientBalance
	ErrContractAddressCollision    = vm.ErrContractAddressCollision
	ErrExecutionReverted           = vm.ErrExecutionReverted
	ErrMaxInitCodeSizeExceeded     = vm.ErrMaxInitCodeSizeExceeded
	ErrMaxCodeSizeExceeded         = vm.ErrMaxCodeSizeExceeded
	ErrInvalidJump                 = vm.ErrInvalidJump
	ErrWriteProtection             = vm.ErrWriteProtection
	ErrReturnDataOutOfBounds       = vm.ErrReturnDataOutOfBounds
	ErrGasUintOverflow             = vm.ErrGasUintOverflow
	ErrInvalidCode                 = vm.ErrInvalidCode
	ErrNonceUintOverflow           = vm.ErrNonceUintOverflow
	ErrAddrProhibited              = errors.New("prohibited address cannot be sender or created contract address")
	ErrInvalidCoinbase             = errors.New("invalid coinbase")
	ErrSenderAddressNotAllowListed = errors.New("cannot issue transaction from non-allow listed address")
)
