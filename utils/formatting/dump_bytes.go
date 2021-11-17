// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"encoding/hex"
	"fmt"
	"strings"
)

var _ fmt.Stringer = DumpBytes{}

type DumpBytes []byte

func (db DumpBytes) String() string { return strings.TrimSpace(hex.Dump(db)) }
