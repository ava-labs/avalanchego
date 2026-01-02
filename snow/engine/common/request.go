// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

type Request struct {
	NodeID    ids.NodeID
	RequestID uint32
}

func (r Request) MarshalText() ([]byte, error) {
	return fmt.Appendf(nil, "%s:%d", r.NodeID, r.RequestID), nil
}
