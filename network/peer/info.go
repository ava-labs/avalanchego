// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"net/netip"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/set"
)

type Info struct {
	IP             netip.AddrPort  `json:"ip"`
	PublicIP       netip.AddrPort  `json:"publicIP,omitempty"`
	ID             ids.NodeID      `json:"nodeID"`
	Version        string          `json:"version"`
	UpgradeTime    uint64          `json:"upgradeTime"`
	LastSent       time.Time       `json:"lastSent"`
	LastReceived   time.Time       `json:"lastReceived"`
	ObservedUptime json.Uint32     `json:"observedUptime"`
	TrackedSubnets set.Set[ids.ID] `json:"trackedSubnets"`
	SupportedACPs  set.Set[uint32] `json:"supportedACPs"`
	ObjectedACPs   set.Set[uint32] `json:"objectedACPs"`
	// MaxFrameSize is the P2P frame size this connection was established on. A
	// value above the default means this node treats the peer as a member of a
	// subnet that declares largeMessages; the default means it does not, which
	// is the first thing to check when a member cannot bootstrap.
	MaxFrameSize uint32 `json:"maxFrameSize"`
}
