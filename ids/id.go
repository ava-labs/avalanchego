// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"bytes"
	"encoding/hex"
	"sort"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// Empty is a useful all zero value
var Empty = ID{}

// ID wraps a 32 byte hash used as an identifier
type ID [32]byte

// ToID attempt to convert a byte slice into an id
func ToID(bytes []byte) (ID, error) {
	return hashing.ToHash256(bytes)
}

// FromString is the inverse of ID.String()
func FromString(idStr string) (ID, error) {
	cb58 := formatting.CB58{}
	if err := cb58.FromString(idStr); err != nil {
		return ID{}, err
	}
	return ToID(cb58.Bytes)
}

// MarshalJSON ...
func (id ID) MarshalJSON() ([]byte, error) {
	return formatting.CB58{Bytes: id[:]}.MarshalJSON()
}

// UnmarshalJSON ...
func (id *ID) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	cb58 := formatting.CB58{}
	if err := cb58.UnmarshalJSON(b); err != nil {
		return err
	}
	newID, err := ToID(cb58.Bytes)
	if err != nil {
		return err
	}
	*id = newID
	return nil
}

// Prefix this id to create a more selective id. This can be used to store
// multiple values under the same key. For example:
// prefix1(id) -> confidence
// prefix2(id) -> vertex
// This will return a new id and not modify the original id.
func (id ID) Prefix(prefixes ...uint64) ID {
	packer := wrappers.Packer{
		Bytes: make([]byte, len(prefixes)*wrappers.LongLen+hashing.HashLen),
	}

	for _, prefix := range prefixes {
		packer.PackLong(prefix)
	}
	packer.PackFixedBytes(id[:])

	return hashing.ComputeHash256Array(packer.Bytes)
}

// Bit returns the bit value at the ith index of the byte array. Returns 0 or 1
func (id ID) Bit(i uint) int {
	byteIndex := i / BitsPerByte
	bitIndex := i % BitsPerByte

	b := id[byteIndex]

	// b = [7, 6, 5, 4, 3, 2, 1, 0]

	b >>= bitIndex

	// b = [0, ..., bitIndex + 1, bitIndex]
	// 1 = [0, 0, 0, 0, 0, 0, 0, 1]

	b &= 1

	// b = [0, 0, 0, 0, 0, 0, 0, bitIndex]

	return int(b)
}

// Hex returns a hex encoded string of this id.
func (id ID) Hex() string { return hex.EncodeToString(id[:]) }

func (id ID) String() string {
	// We assume that the maximum size of a byte slice that
	// can be stringified is at least the length of an ID
	s, _ := formatting.CB58{Bytes: id[:]}.String()
	return s
}

type sortIDData []ID

func (ids sortIDData) Less(i, j int) bool {
	return bytes.Compare(
		ids[i][:],
		ids[j][:]) == -1
}
func (ids sortIDData) Len() int      { return len(ids) }
func (ids sortIDData) Swap(i, j int) { ids[j], ids[i] = ids[i], ids[j] }

// SortIDs sorts the ids lexicographically
func SortIDs(ids []ID) { sort.Sort(sortIDData(ids)) }

// IsSortedAndUniqueIDs returns true if the ids are sorted and unique
func IsSortedAndUniqueIDs(ids []ID) bool { return utils.IsSortedAndUnique(sortIDData(ids)) }
