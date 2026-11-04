// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamic

import (
	"os"
	"strconv"
	"strings"
)

// PlantSetting reads a number from the settlement plant's settings directory (benchmark
// branch only: every node of the local network reads the same files).
func PlantSetting(name string) (uint64, bool) {
	b, err := os.ReadFile("/opt/settlement/chainconf/" + name)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	return v, err == nil
}

// plantJump lets a dynamic parameter reach its vote in one block (the plant starts at its
// gas target and block delay instead of climbing to them over thousands of blocks).
var plantJump = func() bool {
	v, ok := PlantSetting("sae-jump")
	return ok && v == 1
}()
