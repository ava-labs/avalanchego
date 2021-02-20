// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

// BenchedStatus ...
type BenchedStatus struct {
	ChainID ids.ID `json:"chainID"`
	Benched bool   `json:"benched"`
}

// PeerID ...
type PeerID struct {
	IP              string          `json:"ip"`
	PublicIP        string          `json:"publicIP"`
	ID              string          `json:"nodeID"`
	Version         string          `json:"version"`
	LastSent        time.Time       `json:"lastSent"`
	LastReceived    time.Time       `json:"lastReceived"`
	BenchedStatuses []BenchedStatus `json:"benchedStatuses"`
}
