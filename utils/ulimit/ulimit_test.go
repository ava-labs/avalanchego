// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ulimit

import (
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// Test_SetDefault performs sanity checks for the os default.
func Test_SetDefault(t *testing.T) {
	err := Set(DefaultFDLimit, logging.NoLog{})
	if err != nil {
		t.Skipf("default fd-limit failed %v", err)
	}
}
