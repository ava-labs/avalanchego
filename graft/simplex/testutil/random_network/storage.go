// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random_network

import (
	"context"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/testutil"
)

type Storage struct {
	*testutil.InMemStorage
	mempool *Mempool
}

func NewStorage(mempool *Mempool) *Storage {
	return &Storage{
		InMemStorage: testutil.NewInMemStorage(),
		mempool:      mempool,
	}
}

func (s *Storage) Index(ctx context.Context, block common.VerifiedBlock, certificate common.Finalization) error {
	s.mempool.AcceptBlock(block.(*Block))
	return s.InMemStorage.Index(ctx, block, certificate)
}

func (s *Storage) Clone() *Storage {
	return &Storage{
		InMemStorage: s.InMemStorage.Clone(),
		mempool:      s.mempool, // Share the same mempool
	}
}
