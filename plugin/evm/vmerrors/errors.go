// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmerrors

import "errors"

var (
	ErrInvalidCoinbase             = errors.New("invalid coinbase")
	ErrSenderAddressNotAllowListed = errors.New("cannot issue transaction from non-allow listed address")
)
