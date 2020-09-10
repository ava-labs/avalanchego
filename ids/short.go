// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/ava-labs/avalanche-go/utils"
	"github.com/ava-labs/avalanche-go/utils/formatting"
	"github.com/ava-labs/avalanche-go/utils/hashing"
)

// ShortEmpty is a useful all zero value
var ShortEmpty = ShortID{ID: &[20]byte{}}

// ShortID wraps a 20 byte hash as an identifier
type ShortID struct {
	ID *[20]byte `serialize:"true"`
}

// NewShortID creates an identifer from a 20 byte hash
func NewShortID(id [20]byte) ShortID { return ShortID{ID: &id} }

// ToShortID attempt to convert a byte slice into an id
func ToShortID(bytes []byte) (ShortID, error) {
	addrHash, err := hashing.ToHash160(bytes)
	return NewShortID(addrHash), err
}

// ShortFromString is the inverse of ShortID.String()
func ShortFromString(idStr string) (ShortID, error) {
	cb58 := formatting.CB58{}
	err := cb58.FromString(idStr)
	if err != nil {
		return ShortID{}, err
	}
	return ToShortID(cb58.Bytes)
}

// ShortFromPrefixedString returns a ShortID assuming the cb58 format is
// prefixed
func ShortFromPrefixedString(idStr, prefix string) (ShortID, error) {
	if !strings.HasPrefix(idStr, prefix) {
		return ShortID{}, fmt.Errorf("ID: %s is missing the prefix: %s", idStr, prefix)
	}

	return ShortFromString(strings.TrimPrefix(idStr, prefix))
}

// MarshalJSON ...
func (id ShortID) MarshalJSON() ([]byte, error) {
	if id.IsZero() {
		return []byte("null"), nil
	}
	cb58 := formatting.CB58{Bytes: id.ID[:]}
	return cb58.MarshalJSON()
}

// UnmarshalJSON ...
func (id *ShortID) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	cb58 := formatting.CB58{}
	if err := cb58.UnmarshalJSON(b); err != nil {
		return err
	}
	newID, err := ToShortID(cb58.Bytes)
	if err != nil {
		return err
	}
	*id = newID
	return nil
}

// IsZero returns true if the value has not been initialized
func (id ShortID) IsZero() bool { return id.ID == nil }

// LongID returns a 32 byte identifier from this id
func (id ShortID) LongID() ID {
	dest := [32]byte{}
	copy(dest[:], id.ID[:])
	return NewID(dest)
}

// Key returns a 20 byte hash that this id represents. This is useful to allow
// for this id to be used as keys in maps.
func (id ShortID) Key() [20]byte { return *id.ID }

// Equals returns true if the ids have the same byte representation
func (id ShortID) Equals(oID ShortID) bool {
	return id.ID == oID.ID ||
		(id.ID != nil && oID.ID != nil && bytes.Equal(id.Bytes(), oID.Bytes()))
}

// Bytes returns the 20 byte hash as a slice. It is assumed this slice is not
// modified.
func (id ShortID) Bytes() []byte { return id.ID[:] }

// Hex returns a hex encoded string of this id.
func (id ShortID) Hex() string { return hex.EncodeToString(id.Bytes()) }

func (id ShortID) String() string {
	if id.IsZero() {
		return "nil"
	}
	bytes := id.Bytes()
	cb58 := formatting.CB58{Bytes: bytes}
	return cb58.String()
}

// PrefixedString returns the String representation with a prefix added
func (id ShortID) PrefixedString(prefix string) string {
	return prefix + id.String()
}

type sortShortIDData []ShortID

func (ids sortShortIDData) Less(i, j int) bool {
	return bytes.Compare(
		ids[i].Bytes(),
		ids[j].Bytes()) == -1
}
func (ids sortShortIDData) Len() int      { return len(ids) }
func (ids sortShortIDData) Swap(i, j int) { ids[j], ids[i] = ids[i], ids[j] }

// SortShortIDs sorts the ids lexicographically
func SortShortIDs(ids []ShortID) { sort.Sort(sortShortIDData(ids)) }

// IsSortedAndUniqueShortIDs returns true if the ids are sorted and unique
func IsSortedAndUniqueShortIDs(ids []ShortID) bool {
	return utils.IsSortedAndUnique(sortShortIDData(ids))
}

// IsUniqueShortIDs returns true iff [ids] are unique
func IsUniqueShortIDs(ids []ShortID) bool {
	set := ShortSet{}
	set.Add(ids...)
	return set.Len() == len(ids)
}
