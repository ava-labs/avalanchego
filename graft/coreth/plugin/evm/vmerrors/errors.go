// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmerrors

import "errors"

var (
	ErrGenerateBlockFailed     = errors.New("failed to generate block")
	ErrBlockVerificationFailed = errors.New("failed to verify block")
	ErrWrapBlockFailed         = errors.New("failed to wrap block")
)
