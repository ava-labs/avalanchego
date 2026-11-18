// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"testing"

	"github.com/ava-labs/avalanchego/vms/evm/imp/vmtest"
)

func TestConfigResolvePendingToLastExecuted(t *testing.T) {
	vmtest.TestConfigResolvePendingToLastExecuted(t, newSUT)
}
