// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"fmt"

	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/cb58"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

var (
	EmptyNodeID = NodeID(string(EmptyShortNodeID[:]))

	_ utils.Sortable[NodeID] = (*NodeID)(nil)
)

type NodeID string

// Any modification to Bytes will be lost since id is passed-by-value
// Directly access NodeID[:] if you need to modify the NodeID
func (nodeID NodeID) Bytes() []byte {
	return []byte(nodeID)
}

func (nodeID NodeID) String() string {
	// We assume that the maximum size of a byte slice that
	// can be stringified is at least the length of an ID
	str, _ := cb58.Encode([]byte(nodeID))
	return ShortNodeIDPrefix + str
}

func (nodeID NodeID) MarshalJSON() ([]byte, error) {
	return []byte("\"" + nodeID.String() + "\""), nil
}

func (nodeID *NodeID) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == nullStr { // If "null", do nothing
		return nil
	}
	if len(str) <= 2+len(ShortNodeIDPrefix) {
		return fmt.Errorf("%w: expected to be > %d", errShortNodeID, 2+len(ShortNodeIDPrefix))
	}

	lastIndex := len(str) - 1
	if str[0] != '"' || str[lastIndex] != '"' {
		return errMissingQuotes
	}

	var err error
	*nodeID, err = NodeIDFromString(str[1:lastIndex])
	return err
}

func (nodeID NodeID) MarshalText() ([]byte, error) {
	return []byte(nodeID.String()), nil
}

func (nodeID *NodeID) UnmarshalText(text []byte) error {
	return nodeID.UnmarshalJSON(text)
}

func (nodeID NodeID) Less(other NodeID) bool {
	return nodeID < other
}

func NodeIDFromBytes(bytes []byte) NodeID {
	return NodeID(string(bytes))
}

// NodeIDFromShortNodeID attempt to convert a byte slice into a node id
func NodeIDFromShortNodeID(nodeID ShortNodeID) NodeID {
	return NodeID(string(nodeID.Bytes()))
}

func NodeIDFromCert(cert *staking.Certificate) NodeID {
	bytes := hashing.ComputeHash160Array(
		hashing.ComputeHash256(cert.Raw),
	)
	return NodeID(string(bytes[:]))
}

func (nodeID NodeID) ToSize(newSize int) NodeID {
	if len(nodeID) >= newSize {
		return nodeID // leave unchanged if it's longer than required
	}
	pad := make([]byte, newSize-len(nodeID))
	for idx := range pad {
		pad[idx] = 0x00
	}
	bytes := nodeID.Bytes()
	bytes = append(bytes, pad...)
	return NodeIDFromBytes(bytes)
}

// NodeIDFromString is the inverse of NodeID.String()
func NodeIDFromString(nodeIDStr string) (NodeID, error) {
	asShort, err := ShortFromPrefixedString(nodeIDStr, ShortNodeIDPrefix)
	if err != nil {
		return EmptyNodeID, err
	}
	return NodeID(string(asShort.Bytes())), nil
}
