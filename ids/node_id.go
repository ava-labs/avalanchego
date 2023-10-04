// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/cb58"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

const (
	NodeIDPrefix = "NodeID-"

	LongNodeIDLen = 32
)

var (
	EmptyNodeID = NodeID{}

	ErrBadNodeIDLenght = errors.New("bad nodeID length")

	_ utils.Sortable[NodeID] = (*NodeID)(nil)
)

// NodeID embeds a string, rather than being a type alias for a string
// to be able to use custom marshaller for json encoding.
// See https://github.com/golang/go/blob/go1.20.8/src/encoding/json/encode.go#L1004-L1026
// which checks for the string type first, then checks to see if a custom marshaller exists,
// then checks if any other of the primitive types are provided.
type NodeID struct {
	buf string
}

func (n NodeID) Bytes() []byte {
	return []byte(n.buf)
}

func (n NodeID) String() string {
	// We assume that the maximum size of a byte slice that
	// can be stringified is at least the length of an ID
	str, _ := cb58.Encode([]byte(n.buf))
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
	return n.buf < other.buf
}

func ToNodeID(src []byte) (NodeID, error) {
	switch {
	case len(src) == 0:
		return EmptyNodeID, nil

	case len(src) == ShortIDLen || len(src) == LongNodeIDLen:
		bytes := make([]byte, len(src))
		copy(bytes, src)
		return NodeID{
			buf: string(bytes),
		}, nil

	default:
		return EmptyNodeID, fmt.Errorf("expected %d or %d bytes but got %d: %w", ShortNodeIDLen, LongNodeIDLen, len(src), ErrBadNodeIDLenght)
	}
}

func NodeIDFromShortNodeID(nodeID ShortNodeID) NodeID {
	if nodeID == EmptyShortNodeID {
		return EmptyNodeID
	}
	return NodeID{
		buf: string(nodeID.Bytes()),
	}
}

func NodeIDFromCert(cert *staking.Certificate) NodeID {
	return NodeID{
		buf: string(hashing.ComputeHash160(
			hashing.ComputeHash256(cert.Raw),
		)),
	}
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
	return NodeID{
		buf: string(bytes),
	}, nil
}
