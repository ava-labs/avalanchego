// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"bytes"
	"errors"
	"fmt"

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

	errShortNodeID = errors.New("insufficient NodeID length")

	_ utils.Sortable[NodeID] = NodeID{}
)

type NodeID ShortID

// WritableNode is an helper which helps modifiying NodeID content
func WritableNode(id *NodeID) []byte {
	return id[:]
}

func (id NodeID) Bytes() []byte {
	return id[:]
}

// ToNodeID attempt to convert a byte slice into a node id
func ToNodeID(bytes []byte) (NodeID, error) {
	nodeID, err := ToShortID(bytes)
	return NodeID(nodeID), err
}

func (id NodeID) String() string {
	return ShortID(id).PrefixedString(NodeIDPrefix)
}

// NodeIDFromString is the inverse of NodeID.String()
func NodeIDFromString(nodeIDStr string) (NodeID, error) {
	asShort, err := ShortFromPrefixedString(nodeIDStr, NodeIDPrefix)
	if err != nil {
		return EmptyNodeID, err
	}
	return NodeID(asShort), nil
}

// NodeIDFromGenericNodeID is the inverse of NodeID.String()
func NodeIDFromGenericNodeID(genericNodeID GenericNodeID) (NodeID, error) {
	if genericNodeID == EmptyGenericNodeID {
		return EmptyNodeID, nil
	}
	res, err := ToNodeID(genericNodeID.Bytes())
	if err != nil {
		return EmptyNodeID, fmt.Errorf("failed converting GenericNodeID to NodeID, %w", err)
	}
	return res, nil
}

func (id NodeID) MarshalJSON() ([]byte, error) {
	return []byte("\"" + id.String() + "\""), nil
}

func (id *NodeID) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == nullStr { // If "null", do nothing
		return nil
	} else if len(str) <= 2+len(NodeIDPrefix) {
		return fmt.Errorf("%w: expected to be > %d", errShortNodeID, 2+len(NodeIDPrefix))
	}

	lastIndex := len(str) - 1
	if str[0] != '"' || str[lastIndex] != '"' {
		return errMissingQuotes
	}

	var err error
	*id, err = NodeIDFromString(str[1:lastIndex])
	return err
}

func (id NodeID) MarshalText() ([]byte, error) {
	return []byte(id.String()), nil
}

func (id *NodeID) UnmarshalText(text []byte) error {
	return id.UnmarshalJSON(text)
}

func (id NodeID) Less(other NodeID) bool {
	return bytes.Compare(id[:], other[:]) == -1
}

func NodeIDFromCert(cert *staking.Certificate) NodeID {
	return hashing.ComputeHash160Array(
		hashing.ComputeHash256(cert.Raw),
	)
}
