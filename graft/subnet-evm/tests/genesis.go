// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	_ "embed"
)

// Genesis contains the embedded genesis.json file used by tests.
//
//go:embed load/genesis.json
var Genesis []byte
