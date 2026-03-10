// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

// This file MUST be deleted before release. It is intended solely to house
// interim identifiers needed for development over multiple PRs.

import (
	"context"
	"errors"

	"github.com/ava-labs/libevm/common"
)

var errUnimplemented = errors.New("unimplemented")

func (b *apiBackend) GetPoolNonce(context.Context, common.Address) (uint64, error) {
	panic(errUnimplemented)
}
