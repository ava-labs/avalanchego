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
	EmptyGenericNodeID = GenericNodeID(string(EmptyNodeID[:]))

	_ utils.Sortable[GenericNodeID] = (*GenericNodeID)(nil)
)

type GenericNodeID string

func GenericNodeIDFromBytes(bytes []byte) GenericNodeID {
	return GenericNodeID(string(bytes))
}

// GenericNodeIDFromNodeID attempt to convert a byte slice into a node id
func GenericNodeIDFromNodeID(nodeID NodeID) GenericNodeID {
	if nodeID == EmptyNodeID {
		return EmptyGenericNodeID
	}
	return GenericNodeID(string(nodeID.Bytes()))
}

// GenericNodeIDFromString is the inverse of GenericNodeID.String()
func GenericNodeIDFromString(nodeIDStr string) (GenericNodeID, error) {
	asShort, err := ShortFromPrefixedString(nodeIDStr, NodeIDPrefix)
	if err != nil {
		return EmptyGenericNodeID, err
	}
	return GenericNodeID(string(asShort.Bytes())), nil
}

func (nodeID GenericNodeID) Bytes() []byte {
	return []byte(nodeID)
}

func (nodeID GenericNodeID) String() string {
	// We assume that the maximum size of a byte slice that
	// can be stringified is at least the length of an ID
	str, _ := cb58.Encode([]byte(nodeID))
	return NodeIDPrefix + str
}

func (nodeID GenericNodeID) MarshalJSON() ([]byte, error) {
	return []byte("\"" + nodeID.String() + "\""), nil
}

func (nodeID *GenericNodeID) UnmarshalJSON(b []byte) error {
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
	*nodeID, err = GenericNodeIDFromString(str[1:lastIndex])
	return err
}

func (nodeID GenericNodeID) MarshalText() ([]byte, error) {
	return []byte(nodeID.String()), nil
}

func (nodeID *GenericNodeID) UnmarshalText(text []byte) error {
	return nodeID.UnmarshalJSON(text)
}

func (nodeID GenericNodeID) Less(other GenericNodeID) bool {
	return nodeID < other
}

func (nodeID GenericNodeID) Equal(other GenericNodeID) bool {
	return nodeID == other
}

func GenericNodeIDFromCert(cert *staking.Certificate) GenericNodeID {
	bytes := hashing.ComputeHash160Array(
		hashing.ComputeHash256(cert.Raw),
	)
	return GenericNodeID(string(bytes[:]))
}
