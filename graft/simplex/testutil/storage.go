// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/simplex/common"
	"github.com/stretchr/testify/require"
)

type InMemStorage struct {
	data map[uint64]struct {
		common.VerifiedBlock
		common.Finalization
	}

	lock   sync.Mutex
	signal sync.Cond
}

func NewInMemStorage() *InMemStorage {
	s := &InMemStorage{
		data: make(map[uint64]struct {
			common.VerifiedBlock
			common.Finalization
		}),
	}

	s.signal = *sync.NewCond(&s.lock)

	return s
}

func (mem *InMemStorage) NumBlocks() uint64 {
	mem.lock.Lock()
	defer mem.lock.Unlock()

	return uint64(len(mem.data))
}

func (mem *InMemStorage) Clone() *InMemStorage {
	height := mem.NumBlocks()
	return mem.CloneUntil(height)
}

// CloneUntil will copy the storage up until a given sequence(not including).
func (mem *InMemStorage) CloneUntil(stopSeq uint64) *InMemStorage {
	clone := NewInMemStorage()
	for seq := uint64(0); seq < stopSeq; seq++ {
		block, finalization, err := mem.Retrieve(seq)
		if err != nil {
			panic(fmt.Sprintf("failed retrieving block %d: %v", seq, err))
		}
		if err := clone.Index(context.Background(), block, finalization); err != nil {
			panic(fmt.Sprintf("failed indexing block %d: %v", seq, err))
		}
	}
	return clone
}

func (mem *InMemStorage) WaitForBlockCommit(seq uint64) common.VerifiedBlock {
	mem.lock.Lock()
	defer mem.lock.Unlock()

	for {
		if data, exists := mem.data[seq]; exists {
			return data.VerifiedBlock
		}

		mem.signal.Wait()
	}
}

func (mem *InMemStorage) EnsureNoBlockCommit(t *testing.T, seq uint64) {
	require.Never(t, func() bool {
		mem.lock.Lock()
		defer mem.lock.Unlock()

		_, exists := mem.data[seq]
		return exists
	}, time.Second, time.Millisecond*100, "block %d has been committed but shouldn't have been", seq)
}

func (mem *InMemStorage) Retrieve(seq uint64) (common.VerifiedBlock, common.Finalization, error) {
	mem.lock.Lock()
	defer mem.lock.Unlock()

	item, ok := mem.data[seq]
	if !ok {
		return nil, common.Finalization{}, fmt.Errorf("%w: seq %d", common.ErrBlockNotFound, seq)
	}
	return item.VerifiedBlock, item.Finalization, nil
}

func (mem *InMemStorage) Index(ctx context.Context, block common.VerifiedBlock, certificate common.Finalization) error {
	mem.lock.Lock()
	defer mem.lock.Unlock()

	seq := block.BlockHeader().Seq

	_, ok := mem.data[seq]
	if ok {
		panic(fmt.Sprintf("block with seq %d already indexed!", seq))
	}

	if nextSeq := uint64(len(mem.data)); seq != nextSeq {
		panic(fmt.Sprintf("indexing out of order: got seq %d, expected next seq %d", seq, nextSeq))
	}

	mem.data[seq] = struct {
		common.VerifiedBlock
		common.Finalization
	}{block,
		certificate,
	}

	mem.signal.Signal()
	return nil
}

func (mem *InMemStorage) Compare(other *InMemStorage) error {
	mem.lock.Lock()
	defer mem.lock.Unlock()

	other.lock.Lock()
	defer other.lock.Unlock()

	if len(mem.data) != len(other.data) {
		return fmt.Errorf("storage lengths differ: %d vs %d", len(mem.data), len(other.data))
	}

	for seq, item := range mem.data {
		otherItem, ok := other.data[seq]
		if !ok {
			return fmt.Errorf("other storage missing seq %d", seq)
		}

		// compare blocks
		blockBytes := item.Bytes()
		otherBlockBytes := otherItem.Bytes()

		if !bytes.Equal(blockBytes, otherBlockBytes) {
			return fmt.Errorf("blocks differ at seq %d", seq)
		}

		// compare finalizations
		if !bytes.Equal(item.Finalization.Finalization.Bytes(), otherItem.Finalization.Finalization.Bytes()) {
			return fmt.Errorf("finalizations differ at seq %d", seq)
		}
	}

	return nil
}
