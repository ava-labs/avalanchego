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
	ShortNodeIDPrefix = "NodeID-"
	ShortNodeIDLen    = ShortIDLen
)

var (
	EmptyNodeID = ShortNodeID{}

	errShortNodeID = errors.New("insufficient ShortNodeID length")

	_ utils.Sortable[ShortNodeID] = ShortNodeID{}
)

type ShortNodeID ShortID

// WritableNode is an helper which helps modifiying NodeID content
func WritableNode(id *ShortNodeID) []byte {
	return id[:]
}

func (id ShortNodeID) Bytes() []byte {
	return id[:]
}

// ToNodeID attempt to convert a byte slice into a node id
func ToNodeID(bytes []byte) (ShortNodeID, error) {
	nodeID, err := ToShortID(bytes)
	return ShortNodeID(nodeID), err
}

func (id ShortNodeID) String() string {
	return ShortID(id).PrefixedString(ShortNodeIDPrefix)
}

// NodeIDFromString is the inverse of NodeID.String()
func NodeIDFromString(nodeIDStr string) (ShortNodeID, error) {
	asShort, err := ShortFromPrefixedString(nodeIDStr, ShortNodeIDPrefix)
	if err != nil {
		return EmptyNodeID, err
	}
	return ShortNodeID(asShort), nil
}

// NodeIDFromGenericNodeID is the inverse of NodeID.String()
func NodeIDFromGenericNodeID(genericNodeID GenericNodeID) (ShortNodeID, error) {
	if genericNodeID == EmptyGenericNodeID {
		return EmptyNodeID, nil
	}
	res, err := ToNodeID(genericNodeID.Bytes())
	if err != nil {
		return EmptyNodeID, fmt.Errorf("failed converting GenericNodeID to NodeID, %w", err)
	}
	return res, nil
}

func (id ShortNodeID) MarshalJSON() ([]byte, error) {
	return []byte("\"" + id.String() + "\""), nil
}

func (id *ShortNodeID) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == nullStr { // If "null", do nothing
		return nil
	} else if len(str) <= 2+len(ShortNodeIDPrefix) {
		return fmt.Errorf("%w: expected to be > %d", errShortNodeID, 2+len(ShortNodeIDPrefix))
	}

	lastIndex := len(str) - 1
	if str[0] != '"' || str[lastIndex] != '"' {
		return errMissingQuotes
	}

	var err error
	*id, err = NodeIDFromString(str[1:lastIndex])
	return err
}

func (id ShortNodeID) MarshalText() ([]byte, error) {
	return []byte(id.String()), nil
}

func (id *ShortNodeID) UnmarshalText(text []byte) error {
	return id.UnmarshalJSON(text)
}

func (id ShortNodeID) Less(other ShortNodeID) bool {
	return bytes.Compare(id[:], other[:]) == -1
}

func NodeIDFromCert(cert *staking.Certificate) ShortNodeID {
	return hashing.ComputeHash160Array(
		hashing.ComputeHash256(cert.Raw),
	)
}
