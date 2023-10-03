// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"fmt"
	"strings"

	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/cb58"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

const NodeIDPrefix = "NodeID-"

var (
	EmptyNodeID = NodeID(EmptyShortNodeID[:])

	_ utils.Sortable[NodeID] = (*NodeID)(nil)
)

type NodeID string

// Any modification to Bytes will be lost since id is passed-by-value
// Directly access NodeID[:] if you need to modify the NodeID
func (n NodeID) Bytes() []byte {
	return []byte(n)
}

func (n NodeID) String() string {
	// We assume that the maximum size of a byte slice that
	// can be stringified is at least the length of an ID
	str, _ := cb58.Encode([]byte(n))
	return NodeIDPrefix + str
}

func (n NodeID) MarshalJSON() ([]byte, error) {
	return []byte("\"" + n.String() + "\""), nil
}

func (n *NodeID) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == nullStr { // If "null", do nothing
		return nil
	}
	if len(str) <= 2+len(NodeIDPrefix) {
		return fmt.Errorf("%w: expected to be > %d", errShortNodeID, 2+len(NodeIDPrefix))
	}

	lastIndex := len(str) - 1
	if str[0] != '"' || str[lastIndex] != '"' {
		return errMissingQuotes
	}

	var err error
	*n, err = NodeIDFromString(str[1:lastIndex])
	return err
}

func (n NodeID) MarshalText() ([]byte, error) {
	return []byte(n.String()), nil
}

func (n *NodeID) UnmarshalText(text []byte) error {
	return n.UnmarshalJSON(text)
}

func (n NodeID) Less(other NodeID) bool {
	return n < other
}

func NodeIDFromBytes(src []byte, length int) NodeID {
	bytes := make([]byte, length)
	copy(bytes, src)
	return NodeID(bytes)
}

// NodeIDFromShortNodeID attempt to convert a byte slice into a node id
func NodeIDFromShortNodeID(nodeID ShortNodeID) NodeID {
	return NodeID(nodeID.Bytes())
}

func NodeIDFromCert(cert *staking.Certificate) NodeID {
	bytes := hashing.ComputeHash160Array(
		hashing.ComputeHash256(cert.Raw),
	)
	return NodeID(bytes[:])
}

// NodeIDFromString is the inverse of NodeID.String()
func NodeIDFromString(nodeIDStr string) (NodeID, error) {
	if !strings.HasPrefix(nodeIDStr, NodeIDPrefix) {
		return EmptyNodeID, fmt.Errorf("ID: %s is missing the prefix: %s", nodeIDStr, NodeIDPrefix)
	}

	bytes, err := cb58.Decode(strings.TrimPrefix(nodeIDStr, NodeIDPrefix))
	if err != nil {
		return EmptyNodeID, err
	}
	return NodeID(bytes), nil
}
