// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Package core implements the Ethereum consensus protocol.
package core

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/core/types"
)

type blockAndState struct {
	block    *types.Block
	hasState bool
	err      error
}

type blockChainIterator struct {
	bc *BlockChain

	nextReadBlockHeight   uint64
	nextBlockHeightToRead uint64
	blocks                []*blockAndState
	blocksRead            chan *blockAndState
	heightsToRead         chan uint64

	wg        sync.WaitGroup
	closeOnce sync.Once
	onClose   chan struct{}
}

func newBlockChainIterator(bc *BlockChain, start uint64, parallelism int) *blockChainIterator {
	i := &blockChainIterator{
		bc: bc,

		nextReadBlockHeight:   start,
		nextBlockHeightToRead: start,
		blocks:                make([]*blockAndState, parallelism),
		blocksRead:            make(chan *blockAndState),
		heightsToRead:         make(chan uint64),
		onClose:               make(chan struct{}),
	}

	i.wg.Add(parallelism)

	// Start [parallelism] worker threads to read block information
	for j := 0; j < parallelism; j++ {
		// Start a goroutine to read incoming heights from [heightsToRead]
		// fetch the corresponding block information and place it on the
		// [blocksRead] channel.
		go func() {
			defer i.wg.Done()

			for {
				// Read heights in from [heightsToRead]
				var height uint64
				select {
				case height = <-i.heightsToRead:
				case <-i.onClose:
					return
				}

				block := bc.GetBlockByNumber(height)
				if block == nil {
					select {
					case i.blocksRead <- &blockAndState{err: fmt.Errorf("missing block:%d", height)}:
						continue
					case <-i.onClose:
						return
					}
				}

				select {
				case i.blocksRead <- &blockAndState{block: block, hasState: bc.HasState(block.Root())}:
					continue
				case <-i.onClose:
					return
				}
			}
		}()
	}
	lastAccepted := i.bc.LastAcceptedBlock().NumberU64()
	// populateReaders ie. adds task for [parallelism] threads
	i.populateReaders(lastAccepted)
	return i
}

// populateReaders adds the heights for the next [parallelism] blocks to
// [blocksToRead]. This is called piecewise to ensure that each of the blocks
// is read within Next and set in [blocks] before moving on to the next tranche
// of blocks.
func (i *blockChainIterator) populateReaders(lastAccepted uint64) {
	maxHeightToRead := i.nextReadBlockHeight + uint64(len(i.blocks))
	for {
		if i.nextBlockHeightToRead > lastAccepted {
			return
		}
		if maxHeightToRead <= i.nextBlockHeightToRead {
			return
		}
		select {
		case i.heightsToRead <- i.nextBlockHeightToRead:
			i.nextBlockHeightToRead++
		case <-i.onClose:
			return
		}
	}
}

// Next retrieves the next consecutive block in the iteration
func (i *blockChainIterator) Next(ctx context.Context) (*types.Block, bool, error) {
	lastAccepted := i.bc.LastAcceptedBlock().NumberU64()
	if i.nextReadBlockHeight > lastAccepted {
		return nil, false, errors.New("no more blocks")
	}
	i.populateReaders(lastAccepted)

	nextIndex := int(i.nextReadBlockHeight % uint64(len(i.blocks)))
	for {
		nextBlock := i.blocks[nextIndex]
		// If the nextBlock in the iteration has already been populated
		// return the block immediately.
		if nextBlock != nil {
			i.blocks[nextIndex] = nil
			i.nextReadBlockHeight++
			i.populateReaders(lastAccepted)
			return nextBlock.block, nextBlock.hasState, nil
		}

		// Otherwise, keep reading in block info from [blocksRead]
		// and populate the [blocks] buffer until we hit the actual
		// next block in the iteration.
		select {
		case block := <-i.blocksRead:
			if block.err != nil {
				i.Stop()
				return nil, false, block.err
			}

			index := int(block.block.NumberU64() % uint64(len(i.blocks)))
			i.blocks[index] = block
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-i.onClose:
			return nil, false, errors.New("closed")
		}
	}
}

// Stop closes the [onClose] channel signalling all worker threads to exit
// and waits for all of the worker threads to finish.
func (i *blockChainIterator) Stop() {
	i.closeOnce.Do(func() {
		close(i.onClose)
	})
	i.wg.Wait()
}
