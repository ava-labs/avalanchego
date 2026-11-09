// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	RegisterExtras()
	os.Exit(m.Run())
}
