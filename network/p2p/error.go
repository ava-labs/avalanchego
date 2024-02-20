// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import "github.com/ava-labs/avalanchego/snow/engine/common"

var (
	// ErrUnexpected should be used to indicate that a request failed due to a
	// generic error
	ErrUnexpected = &common.AppError{
		Code: -1,
	}
	// ErrUnregisteredHandler should be used to indicate that a request failed
	// due to it not matching a registered handler
	ErrUnregisteredHandler = &common.AppError{
		Code: -2,
	}
	// ErrNotValidator should be used to indicate that a request failed due to
	// the requesting peer not being a validator
	ErrNotValidator = &common.AppError{
		Code: -3,
	}
	// ErrThrottled should be used to indicate that a request failed due to the
	// requesting peer exceeding a rate limit
	ErrThrottled = &common.AppError{
		Code: -4,
	}
)
