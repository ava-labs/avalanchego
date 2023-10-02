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
	EmptyShortNodeID = ShortNodeID{}

	errShortNodeID = errors.New("insufficient ShortNodeID length")

	_ utils.Sortable[ShortNodeID] = ShortNodeID{}
)

type ShortNodeID ShortID

func (sn ShortNodeID) Bytes() []byte {
	return sn[:]
}

// ToShortNodeID attempt to convert a byte slice into a node id
func ToShortNodeID(bytes []byte) (ShortNodeID, error) {
	nodeID, err := ToShortID(bytes)
	return ShortNodeID(nodeID), err
}

func (sn ShortNodeID) String() string {
	return ShortID(sn).PrefixedString(ShortNodeIDPrefix)
}

// ShortNodeIDFromString is the inverse of NodeID.String()
func ShortNodeIDFromString(nodeIDStr string) (ShortNodeID, error) {
	asShort, err := ShortFromPrefixedString(nodeIDStr, ShortNodeIDPrefix)
	if err != nil {
		return EmptyShortNodeID, err
	}
	return ShortNodeID(asShort), nil
}

// ShortNodeIDFromNodeID is the inverse of NodeID.String()
func ShortNodeIDFromNodeID(nodeID NodeID) (ShortNodeID, error) {
	if nodeID == EmptyNodeID {
		return EmptyShortNodeID, nil
	}
	res, err := ToShortNodeID(nodeID.Bytes())
	if err != nil {
		return EmptyShortNodeID, fmt.Errorf("failed converting NodeID to ShortNodeID, %w", err)
	}
	return res, nil
}

func (sn ShortNodeID) MarshalJSON() ([]byte, error) {
	return []byte("\"" + sn.String() + "\""), nil
}

func (sn *ShortNodeID) UnmarshalJSON(b []byte) error {
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
	*sn, err = ShortNodeIDFromString(str[1:lastIndex])
	return err
}

func (sn ShortNodeID) MarshalText() ([]byte, error) {
	return []byte(sn.String()), nil
}

func (sn *ShortNodeID) UnmarshalText(text []byte) error {
	return sn.UnmarshalJSON(text)
}

func (sn ShortNodeID) Less(other ShortNodeID) bool {
	return bytes.Compare(sn[:], other[:]) == -1
}

func ShortNodeIDFromCert(cert *staking.Certificate) ShortNodeID {
	return hashing.ComputeHash160Array(
		hashing.ComputeHash256(cert.Raw),
	)
}
