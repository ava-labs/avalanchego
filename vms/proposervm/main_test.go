// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain uses goleak to verify tests in this package do not leak unexpected
// goroutines.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
