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

var (
	EmptyGenericNodeID = GenericNodeID{
		id: string(EmptyNodeID[:]),
	}

	_ utils.Sortable[GenericNodeID] = (*GenericNodeID)(nil)
)

type GenericNodeID struct {
	// id is a string (instead of a []byte) to make GenericNodeID comparable
	id string // TODO ABENGIA: consider exporting for serialization
}

// GenericNodeIDFromNodeID attempt to convert a byte slice into a node id
func GenericNodeIDFromNodeID(nodeID NodeID) GenericNodeID {
	if nodeID == EmptyNodeID {
		return EmptyGenericNodeID
	}
	return GenericNodeID{
		id: strings.Clone(string(nodeID.Bytes())),
	}
}

// GenericNodeIDFromString is the inverse of GenericNodeID.String()
func GenericNodeIDFromString(nodeIDStr string) (GenericNodeID, error) {
	asShort, err := ShortFromPrefixedString(nodeIDStr, NodeIDPrefix)
	if err != nil {
		return GenericNodeID{}, err
	}
	return GenericNodeID{id: string(asShort.Bytes())}, nil
}

// WritableGenericNode is an helper which helps modifiying GenericNodeID content
func WritableGenericNode(nodeID GenericNodeID) []byte {
	return []byte(nodeID.id)
}

func (nodeID *GenericNodeID) Bytes() []byte {
	res := strings.Clone(nodeID.id)
	return []byte(res)
}

func (nodeID *GenericNodeID) String() string {
	// We assume that the maximum size of a byte slice that
	// can be stringified is at least the length of an ID
	str, _ := cb58.Encode([]byte(nodeID.id))
	return NodeIDPrefix + str
}

func (nodeID *GenericNodeID) MarshalJSON() ([]byte, error) {
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

func (nodeID *GenericNodeID) MarshalText() ([]byte, error) {
	return []byte(nodeID.String()), nil
}

func (nodeID *GenericNodeID) UnmarshalText(text []byte) error {
	return nodeID.UnmarshalJSON(text)
}

func (nodeID *GenericNodeID) Less(other GenericNodeID) bool {
	return nodeID.id < other.id
}

func (nodeID *GenericNodeID) Equal(other GenericNodeID) bool {
	return nodeID.id == other.id
}

func GenericNodeIDFromCert(cert *staking.Certificate) GenericNodeID {
	bytes := hashing.ComputeHash160Array(
		hashing.ComputeHash256(cert.Raw),
	)
	return GenericNodeID{id: string(bytes[:])}
}
