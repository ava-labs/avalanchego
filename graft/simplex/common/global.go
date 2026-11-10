// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

var errLongNodeID = errors.New("long node IDs cannot be converted to an avalanchego node ID")

// NodeID is a unique identifier for a node in the network.
// It consists of a ShortID and a LongID for backward compatibility.
// The LongID has preferences over the ShortID.
type NodeID struct {
	// ShortID is a 20-byte identifier for the node, used for legacy node IDs.
	ShortID ids.ShortID
	// LongID is a 32-byte identifier for the node, used for future implementations.
	LongID [32]byte
}

// NodeIDFromAvalancheID converts an avalanchego node ID to a NodeID.
func NodeIDFromAvalancheID(nodeID ids.NodeID) NodeID {
	return NodeID{ShortID: ids.ShortID(nodeID)}
}

// ToAvalancheID converts the NodeID to an avalanchego node ID.
// A NodeID with a populated LongID cannot be represented as an ids.NodeID.
func (node NodeID) ToAvalancheID() (ids.NodeID, error) {
	if node.LongID != ([32]byte{}) {
		return ids.EmptyNodeID, errLongNodeID
	}
	return ids.NodeID(node.ShortID), nil
}

func (node NodeID) Compare(other NodeID) int {
	if cmp := bytes.Compare(node.LongID[:], other.LongID[:]); cmp != 0 {
		return cmp
	}
	return bytes.Compare(node.ShortID[:], other.ShortID[:])
}

// String returns the string representation of the NodeID.
// If the LongID is empty, it returns the ShortID in hex format.
func (node NodeID) String() string {
	var emptyLongID [32]byte
	if bytes.Equal(emptyLongID[:], node.LongID[:]) {
		return hex.EncodeToString(node.ShortID[:])
	}
	return hex.EncodeToString(node.LongID[:])
}

func (node NodeID) Equals(otherNode NodeID) bool {
	return node == otherNode
}

type NodeIDs []NodeID

func (nodes NodeIDs) String() string {
	var nodeStrings []string
	for _, node := range nodes {
		nodeStrings = append(nodeStrings, node.String())
	}
	return fmt.Sprintf("%v", nodeStrings)
}

func (nodes NodeIDs) Remove(targetNode NodeID) []NodeID {
	for i, n := range nodes {
		if n.Equals(targetNode) {
			result := make([]NodeID, 0, len(nodes)-1)
			result = append(result, nodes[:i]...)
			result = append(result, nodes[i+1:]...)
			return result
		}
	}
	return nodes
}

func (nodes NodeIDs) IndexOf(id NodeID) int {
	for i, n := range nodes {
		if n.Equals(id) {
			return i
		}
	}
	return -1
}

func (nodes NodeIDs) EqualWeightedNodes() Nodes {
	weights := make(Nodes, len(nodes))
	for i, node := range nodes {
		weights[i] = Node{
			Id:     node,
			Weight: 1,
		}
	}
	return weights
}
