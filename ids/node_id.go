// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"errors"

	"github.com/ava-labs/avalanchego/utils"
)

const (
	NodeIDPrefix = "NodeID-"
	NodeIDLen    = ShortIDLen // this would eventually be updated to 32 byte length.
)

var (
	EmptyNodeID = NodeID{}

	_ utils.Sortable[NodeID] = NodeID{}

	errWrongNodeIDLength = errors.New("wrong NodeID length")
)

type NodeID struct {
	ShortNodeID `serialize:"true"`
}

func (id NodeID) Compare(other NodeID) int {
	return id.ShortNodeID.Compare(other.ShortNodeID)
}

// NodeIDFromString is the inverse of NodeID.String()
func NodeIDFromString(nodeIDStr string) (NodeID, error) {
	asShort, err := ShortFromPrefixedString(nodeIDStr, NodeIDPrefix)
	if err != nil {
		return NodeID{}, err
	}
	return NodeID{ShortNodeID: ShortNodeID(asShort)}, nil
}

func ToNodeID(bytes []byte) (NodeID, error) {
	nodeID, err := ToShortID(bytes)
	return NodeID{ShortNodeID: ShortNodeID(nodeID)}, err
}

func ParseNodeID(bytes []byte) (NodeID, error) {
	if len(bytes) == ShortIDLen {
		var node NodeID
		copy(node.ShortNodeID[:], bytes)
		return node, nil
	}
	return NodeID{}, errWrongNodeIDLength
}
