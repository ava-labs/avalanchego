// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"bytes"

	"github.com/ava-labs/avalanchego/ids"
)

type validatorData struct {
	id     ids.NodeID
	weight uint64
}

func (d validatorData) Less(other validatorData) bool {
	return bytes.Compare(d.id[:], other.id[:]) == -1
}
