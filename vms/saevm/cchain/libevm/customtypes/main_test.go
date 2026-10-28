// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	Register()
	os.Exit(m.Run())
}
