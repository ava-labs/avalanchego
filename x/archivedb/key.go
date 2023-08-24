package archivedb

import (
	"errors"
	"math"
)

var errBadVersion = errors.New("invalid version")

// Key
//
// The keys contains a few extra information alongside the given key. The key is
// what is known outside of this internal scope as the key, but this struct has
// more information compacted inside the Key to take advantage of natural
// sorting. The inversed height is stored right after the Prefix, which is
// MaxUint64 minus the desired Height. An extra byte is also packed inside the
// key to indicate if the key is a deletion of any previous value (because
// archivedb is an append only database).
//
// Any other property are to not serialized but they are useful when parsing a
// Key struct from the database
type Key struct {
	Prefix         []byte `serialize:"true"`
	HeightReversed uint64 `serialize:"true"`
	IsDeleted      bool   `serialize:"true"`
	Height         uint64
	Bytes          []byte
}

// Creates a new Key struct with a given key and its height
func NewKey(key []byte, height uint64) (Key, error) {
	internalKey := Key{
		Prefix:         key,
		HeightReversed: math.MaxUint64 - height,
		IsDeleted:      false,
		Height:         height,
	}

	bytes, err := Codec.Marshal(Version, &internalKey)
	if err != nil {
		return Key{}, err
	}

	internalKey.Bytes = bytes

	return internalKey, nil
}

// Flag the current Key sturct as a deletion. If this happens the entry Value is
// not important and it will usually an empty vector of bytes
func (k *Key) SetDeleted() error {
	k.IsDeleted = true
	bytes, err := Codec.Marshal(Version, &k)
	if err != nil {
		return err
	}
	k.Bytes = bytes
	return nil
}

// Takes a vector of bytes and returns a Key struct
func ParseKey(keyBytes []byte) (Key, error) {
	var key Key
	version, err := Codec.Unmarshal(keyBytes, &key)
	if err != nil {
		return Key{}, err
	}
	if version != Version {
		return Key{}, errBadVersion
	}

	key.Height = math.MaxUint64 - key.HeightReversed
	key.Bytes = keyBytes

	return key, nil
}
