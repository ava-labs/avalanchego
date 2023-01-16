// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package multisig

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/types"
)

type Alias struct {
	ID     ids.ShortID
	Memo   types.JSONByteSlice `serialize:"true" json:"memo"`
	Owners verify.Verifiable   `serialize:"true" json:"owners"`
}
