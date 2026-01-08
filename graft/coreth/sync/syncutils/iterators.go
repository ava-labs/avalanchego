// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncutils

import (
	"sync"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/avalanchego/graft/coreth/core/state/snapshot"
)

const (
	// accountBatchSize is the number of accounts to encode in parallel
	accountBatchSize = 16
	// encoderWorkers is the number of goroutines for parallel RLP encoding
	encoderWorkers = 4
)

var (
	_ ethdb.Iterator = (*AccountIterator)(nil)
	_ ethdb.Iterator = (*StorageIterator)(nil)

	// rlpEncoderPool reuses encoder workers across iterators
	rlpEncoderPool = sync.Pool{
		New: func() interface{} {
			return make(chan *encodeJob, accountBatchSize)
		},
	}
)

// encodeJob represents a single account RLP encoding task
type encodeJob struct {
	account []byte // RLP-encoded account data from snapshot
	hash    []byte
	result  []byte
	err     error
	done    chan struct{}
}

// AccountIterator wraps a [snapshot.AccountIterator] to conform to [ethdb.Iterator]
// accounts will be returned in consensus (FullRLP) format for compatibility with trie data.
// Uses parallel RLP encoding with read-ahead buffering for improved CPU utilization.
type AccountIterator struct {
	snapshot.AccountIterator
	err    error
	val    []byte
	buffer []*encodeJob // Pre-encoded accounts buffer
	bufIdx int          // Current position in buffer
}

func (it *AccountIterator) Next() bool {
	if it.err != nil {
		return false
	}

	// Check if we have buffered results
	if it.bufIdx < len(it.buffer) {
		job := it.buffer[it.bufIdx]
		it.bufIdx++
		it.val = job.result
		it.err = job.err
		return it.err == nil
	}

	// Buffer exhausted - fill next batch
	it.buffer = it.buffer[:0]
	it.bufIdx = 0

	// Collect next batch of accounts
	jobs := make([]*encodeJob, 0, accountBatchSize)
	for len(jobs) < accountBatchSize && it.AccountIterator.Next() {
		acc := it.Account()
		job := &encodeJob{
			account: acc,
			hash:    it.Hash().Bytes(),
			done:    make(chan struct{}),
		}
		jobs = append(jobs, job)
	}

	if len(jobs) == 0 {
		it.val = nil
		return false
	}

	// Encode batch in parallel using worker pool
	var wg sync.WaitGroup
	jobChan := make(chan *encodeJob, len(jobs))

	// Start encoder workers
	for range min(encoderWorkers, len(jobs)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobChan {
				job.result, job.err = types.FullAccountRLP(job.account)
				close(job.done)
			}
		}()
	}

	// Send jobs to workers
	for _, job := range jobs {
		jobChan <- job
	}
	close(jobChan)

	// Wait for all encodings to complete
	wg.Wait()

	// Store results in buffer
	it.buffer = jobs

	// Return first result
	if len(it.buffer) > 0 {
		job := it.buffer[0]
		it.bufIdx = 1
		it.val = job.result
		it.err = job.err
		return it.err == nil
	}

	return false
}

func (it *AccountIterator) Key() []byte {
	if it.err != nil {
		return nil
	}
	// Return key from buffer if available, otherwise from current iterator position
	if it.bufIdx > 0 && it.bufIdx <= len(it.buffer) {
		return it.buffer[it.bufIdx-1].hash
	}
	return it.Hash().Bytes()
}

func (it *AccountIterator) Value() []byte {
	if it.err != nil {
		return nil
	}
	return it.val
}

func (it *AccountIterator) Error() error {
	if it.err != nil {
		return it.err
	}
	return it.AccountIterator.Error()
}

// StorageIterator wraps a [snapshot.StorageIterator] to conform to [ethdb.Iterator]
type StorageIterator struct {
	snapshot.StorageIterator
}

func (it *StorageIterator) Key() []byte {
	return it.Hash().Bytes()
}

func (it *StorageIterator) Value() []byte {
	return it.Slot()
}
