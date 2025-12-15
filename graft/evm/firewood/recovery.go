// Copyright 2025 the libevm authors.
//
// The libevm additions to go-ethereum are free software: you can redistribute
// them and/or modify them under the terms of the GNU Lesser General Public License
// as published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The libevm additions are distributed in the hope that they will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Lesser
// General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see
// <http://www.gnu.org/licenses/>.

// The firewood package provides a [triedb.DBOverride] backed by [Firewood].
//
// [Firewood]: https://github.com/ava-labs/firewood
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
