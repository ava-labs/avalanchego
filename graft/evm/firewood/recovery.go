// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
)

const (
	committedBlockHashKey = "committedFirewoodBlockHash"
	committedHeightKey    = "committedFirewoodHeight"
)

// ReadCommittedBlockHash retrieves the most recently committed block hash from the key-value store.
func ReadCommittedBlockHashes(kvStore ethdb.Database) (map[common.Hash]struct{}, error) {
	data, _ := kvStore.Get([]byte(committedBlockHashKey)) // ignore not found error
	if len(data)%common.HashLength != 0 {
		return nil, fmt.Errorf("invalid committed block hash length: expected multiple of %d, got %d", common.HashLength, len(data))
	}
	hashes := make(map[common.Hash]struct{})
	if len(data) == 0 {
		hashes[common.Hash{}] = struct{}{}
		return hashes, nil
	}
	for i := 0; i < len(data); i += common.HashLength {
		hash := common.BytesToHash(data[i : i+common.HashLength])
		hashes[hash] = struct{}{}
	}
	return hashes, nil
}

// WriteCommittedBlockHash writes the most recently committed block hash to the key-value store.
func WriteCommittedBlockHashes(kvStore ethdb.Database, hashes map[common.Hash]struct{}) error {
	contents := make([]byte, 0, len(hashes)*common.HashLength)
	for hash := range hashes {
		contents = append(contents, hash.Bytes()...)
	}
	if err := kvStore.Put([]byte(committedBlockHashKey), contents); err != nil {
		return fmt.Errorf("error writing committed block hashes: %w", err)
	}
	return nil
}

// ReadCommittedHeight retrieves the most recently committed height from the key-value store.
func ReadCommittedHeight(kvStore ethdb.Database) uint64 {
	data, _ := kvStore.Get([]byte(committedHeightKey))
	if len(data) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(data)
}

// WriteCommittedHeight writes the most recently committed height to the key-value store.
func WriteCommittedHeight(kvStore ethdb.Database, height uint64) error {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, height)
	if err := kvStore.Put([]byte(committedHeightKey), enc); err != nil {
		return fmt.Errorf("error writing committed height: %w", err)
	}
	return nil
}
