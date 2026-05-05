// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"os"
	"testing"

	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
)

// TestMain registers libevm extras required by [params.GetRulesExtra] and
// predicate tests in this package.
func TestMain(m *testing.M) {
	core.RegisterExtras()
	params.RegisterExtras()
	os.Exit(m.Run())
}
