// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

const (
	NodeIDPrefix = "NodeID-"
	NodeIDLen    = ShortIDLen
)

var (
	EmptyNodeID = NodeID{}

	_ utils.Sortable[NodeID] = NodeID{}
)

type NodeID struct {
	ShortNodeID
}

// ToNodeID attempt to convert a byte slice into a node id
func ToNodeID(bytes []byte) (NodeID, error) {
	nodeID, err := ToShortID(bytes)
	return NodeID{ShortNodeID: ShortNodeID(nodeID)}, err
}

func (id NodeID) Compare(other NodeID) int {
	return id.ShortNodeID.Compare(other.ShortNodeID)
}

func NodeIDFromCert(cert *staking.Certificate) NodeID {
	return NodeID{
		ShortNodeID: ShortNodeID(hashing.ComputeHash160Array(hashing.ComputeHash256(cert.Raw))),
	}
}

// NodeIDFromString is the inverse of NodeID.String()
func NodeIDFromString(nodeIDStr string) (NodeID, error) {
	asShort, err := ShortFromPrefixedString(nodeIDStr, NodeIDPrefix)
	if err != nil {
		return NodeID{}, err
	}
	return NodeID{ShortNodeID: ShortNodeID(asShort)}, nil
}
