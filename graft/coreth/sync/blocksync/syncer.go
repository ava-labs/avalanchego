// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocksync

import (
	"context"
	"errors"
	"fmt"

	synccommon "github.com/ava-labs/coreth/sync"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
)

const blocksPerRequest = 32

var (
	_ synccommon.Syncer = (*blockSyncer)(nil)

	errNilClient       = errors.New("Client cannot be nil")
	errNilDatabase     = errors.New("Database cannot be nil")
	errInvalidFromHash = errors.New("FromHash cannot be empty")
)

type Config struct {
	ChainDB       ethdb.Database
	Client        statesyncclient.Client
	FromHash      common.Hash // `FromHash` is the most recent
	FromHeight    uint64
	BlocksToFetch uint64 // Includes the `FromHash` block
}

func (c *Config) Validate() error {
	if c.ChainDB == nil {
		return errNilDatabase
	}
	if c.Client == nil {
		return errNilClient
	}
	if c.FromHash == (common.Hash{}) {
		return errInvalidFromHash
	}
	return nil
}

type blockSyncer struct {
	config *Config

	err    chan error
	cancel context.CancelFunc
}

func NewSyncer(config *Config) (*blockSyncer, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &blockSyncer{
		config: config,
		err:    make(chan error, 1),
	}, nil
}

func (s *blockSyncer) Start(ctx context.Context) error {
	if s.cancel != nil {
		return synccommon.ErrSyncerAlreadyStarted
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	go func() {
		s.err <- s.sync(cancelCtx)
	}()
	return nil
}

func (s *blockSyncer) Wait(ctx context.Context) error {
	if s.cancel == nil {
		return synccommon.ErrWaitBeforeStart
	}

	select {
	case err := <-s.err:
		return err
	case <-ctx.Done():
		s.cancel()
		<-s.err // wait for the syncer to finish
		return ctx.Err()
	}
}

// sync fetches (up to) BlocksToFetch blocks from peers
// using Client and writes them to disk.
// the process begins with FromHash and it fetches parents recursively.
// fetching starts from the first ancestor not found on disk
//
// TODO: We could inspect the database more accurately to ensure we never fetch
// any blocks that are locally available.
// We could also prevent overrequesting blocks, if the number of blocks needed
// to be fetched isn't a multiple of blocksPerRequest.
func (s *blockSyncer) sync(ctx context.Context) error {
	nextHash := s.config.FromHash
	nextHeight := s.config.FromHeight
	blocksToFetch := s.config.BlocksToFetch

	// first, check for blocks already available on disk so we don't
	// request them from peers.
	for blocksToFetch > 0 {
		blk := rawdb.ReadBlock(s.config.ChainDB, nextHash, nextHeight)
		if blk == nil {
			// block was not found
			break
		}

		// block exists
		nextHash = blk.ParentHash()
		nextHeight--
		blocksToFetch--
	}

	// get any blocks we couldn't find on disk from peers and write
	// them to disk.
	batch := s.config.ChainDB.NewBatch()
	for fetched := uint64(0); fetched < blocksToFetch && (nextHash != common.Hash{}); {
		log.Info("fetching blocks from peer", "fetched", fetched, "total", blocksToFetch)
		blocks, err := s.config.Client.GetBlocks(ctx, nextHash, nextHeight, blocksPerRequest)
		if err != nil {
			return fmt.Errorf("could not get blocks from peer: err: %w, nextHash: %s, fetched: %d", err, nextHash, fetched)
		}
		for _, block := range blocks {
			rawdb.WriteBlock(batch, block)
			rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())

			fetched++
			nextHash = block.ParentHash()
			nextHeight--
		}
	}

	log.Info("fetched blocks from peer", "total", blocksToFetch)
	return batch.Write()
}
