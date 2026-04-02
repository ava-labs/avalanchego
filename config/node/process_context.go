// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import "net/netip"

type ProcessContext struct {
	// The process id of the node
	PID int `json:"pid"`
	// URI to access the node API
	// Format: [https|http]://[host]:[port]
	URI string `json:"uri"`
	// Address other nodes can use to communicate with this node
	StakingAddress netip.AddrPort `json:"stakingAddress"`
}
