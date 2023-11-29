// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import "github.com/ava-labs/avalanchego/ids"

type Request struct {
	NodeID    ids.NodeID
	RequestID uint32
}
