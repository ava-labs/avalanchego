// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"encoding/hex"
	"strings"
)

// DumpBytes ...
type DumpBytes struct{ Bytes []byte }

func (db DumpBytes) String() string { return strings.TrimSpace(hex.Dump(db.Bytes)) }
