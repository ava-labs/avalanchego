// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"go.uber.org/goleak"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
)

// TestMain registers libevm extras required by [params.GetRulesExtra] and
// predicate tests in this package.
func TestMain(m *testing.M) {
	core.RegisterExtras()
	params.RegisterExtras()
	goleak.VerifyTestMain(m, goleak.IgnoreCurrent())
}
