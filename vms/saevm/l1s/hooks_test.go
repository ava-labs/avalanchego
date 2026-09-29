// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"testing"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/vms/saevm/hook/hookstest"
)

func TestBlockTime(t *testing.T) {
	_, sut := newSUT(t)
	hookstest.TestBlockTime(t, sut.hooks(t), func(h *types.Header, ms uint64) *types.Header {
		return customtypes.WithHeaderExtra(h, &customtypes.HeaderExtra{TimeMilliseconds: &ms})
	})
}

func TestSettledBy(t *testing.T) {
	_, sut := newSUT(t)
	hookstest.TestSettledBy(t, sut.hooks(t), []*types.Transaction{types.NewTx(&types.LegacyTx{})}, nil)
}
