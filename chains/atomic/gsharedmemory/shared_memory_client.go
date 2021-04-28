// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gsharedmemory

import (
	"context"

	stdatomic "sync/atomic"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/chains/atomic/gsharedmemory/gsharedmemoryproto"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

const (
	maxBatchSize = 64 * 1024 // 64 KiB

	// baseElementSize is an approximation of the protobuf encoding overhead per
	// element
	baseElementSize = 8 // bytes
)

var (
	_ atomic.SharedMemory = &Client{}
)

// Client is atomic.SharedMemory that talks over RPC.
type Client struct {
	client gsharedmemoryproto.SharedMemoryClient

	uniqueID int64
}

// NewClient returns shared memory connected to remote shared memory
func NewClient(client gsharedmemoryproto.SharedMemoryClient) *Client {
	return &Client{client: client}
}

func (c *Client) Put(peerChainID ids.ID, elems []*atomic.Element, rawBatches ...database.Batch) error {
	req := gsharedmemoryproto.PutRequest{
		PeerChainID: peerChainID[:],
		Elems:       make([]*gsharedmemoryproto.Element, len(elems)),
		Id:          stdatomic.AddInt64(&c.uniqueID, 1),
		Continues:   true,
	}

	currentSize := 0
	for i, elem := range elems {
		currentSize += baseElementSize + len(elem.Key) + len(elem.Value)
		for _, trait := range elem.Traits {
			currentSize += len(trait)
		}

		req.Elems[i] = &gsharedmemoryproto.Element{
			Key:    elem.Key,
			Value:  elem.Value,
			Traits: elem.Traits,
		}
	}

	batchGroups, err := c.makeBatches(rawBatches, currentSize)
	if err != nil {
		return err
	}

	for i, batches := range batchGroups {
		req.Batches = batches
		req.Continues = i < len(batchGroups)-1
		if _, err := c.client.Put(context.Background(), &req); err != nil {
			return err
		}
		req.PeerChainID = nil
		req.Elems = nil
	}
	if len(batchGroups) == 0 {
		req.Continues = false
		if _, err := c.client.Put(context.Background(), &req); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) Get(peerChainID ids.ID, keys [][]byte) (values [][]byte, err error) {
	resp, err := c.client.Get(context.Background(), &gsharedmemoryproto.GetRequest{
		PeerChainID: peerChainID[:],
		Keys:        keys,
	})
	if err != nil {
		return nil, err
	}
	return resp.Values, nil
}

func (c *Client) Indexed(
	peerChainID ids.ID,
	traits [][]byte,
	startTrait,
	startKey []byte,
	limit int,
) (
	values [][]byte,
	lastTrait,
	lastKey []byte,
	err error,
) {
	resp, err := c.client.Indexed(context.Background(), &gsharedmemoryproto.IndexedRequest{
		PeerChainID: peerChainID[:],
		Traits:      traits,
		StartTrait:  startTrait,
		StartKey:    startKey,
		Limit:       int32(limit),
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return resp.Values, resp.LastTrait, resp.LastKey, nil
}

func (c *Client) Remove(peerChainID ids.ID, keys [][]byte, rawBatches ...database.Batch) error {
	req := gsharedmemoryproto.RemoveRequest{
		PeerChainID: peerChainID[:],
		Keys:        keys,
		Id:          stdatomic.AddInt64(&c.uniqueID, 1),
		Continues:   true,
	}

	currentSize := 0
	for _, key := range keys {
		currentSize += baseElementSize + len(key)
	}

	batchGroups, err := c.makeBatches(rawBatches, currentSize)
	if err != nil {
		return err
	}

	for i, batches := range batchGroups {
		req.Batches = batches
		req.Continues = i < len(batchGroups)-1
		if _, err := c.client.Remove(context.Background(), &req); err != nil {
			return err
		}
		req.PeerChainID = nil
		req.Keys = nil
	}
	if len(batchGroups) == 0 {
		req.Continues = false
		if _, err := c.client.Remove(context.Background(), &req); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) makeBatches(rawBatches []database.Batch, currentSize int) ([][]*gsharedmemoryproto.Batch, error) {
	batchGroups := [][]*gsharedmemoryproto.Batch(nil)
	currentBatchGroup := []*gsharedmemoryproto.Batch(nil)
	currentBatch := &gsharedmemoryproto.Batch{
		Id: stdatomic.AddInt64(&c.uniqueID, 1),
	}
	for _, batch := range rawBatches {
		batch := batch.Inner()
		fb := filteredBatch{
			writes:  make(map[string][]byte),
			deletes: make(map[string]struct{}),
		}
		if err := batch.Replay(&fb); err != nil {
			return nil, err
		}

		puts := fb.PutRequests()
		for _, p := range puts {
			sizeChange := baseElementSize + len(p.Key) + len(p.Value)
			if newSize := currentSize + sizeChange; newSize > maxBatchSize {
				if len(currentBatch.Deletes)+len(currentBatch.Puts) > 0 {
					currentBatchGroup = append(currentBatchGroup, currentBatch)
				}
				if len(currentBatchGroup) > 0 {
					batchGroups = append(batchGroups, currentBatchGroup)
				}

				currentSize = 0
				currentBatchGroup = nil
				currentBatch = &gsharedmemoryproto.Batch{
					Id: currentBatch.Id,
				}
			}
			currentSize += sizeChange
			currentBatch.Puts = append(currentBatch.Puts, p)
		}

		deletes := fb.DeleteRequests()
		for _, d := range deletes {
			sizeChange := baseElementSize + len(d.Key)
			if newSize := currentSize + sizeChange; newSize > maxBatchSize {
				if len(currentBatch.Deletes)+len(currentBatch.Puts) > 0 {
					currentBatchGroup = append(currentBatchGroup, currentBatch)
				}
				if len(currentBatchGroup) > 0 {
					batchGroups = append(batchGroups, currentBatchGroup)
				}

				currentSize = 0
				currentBatchGroup = nil
				currentBatch = &gsharedmemoryproto.Batch{
					Id: currentBatch.Id,
				}
			}
			currentSize += sizeChange
			currentBatch.Deletes = append(currentBatch.Deletes, d)
		}

		if len(currentBatch.Deletes)+len(currentBatch.Puts) > 0 {
			currentBatchGroup = append(currentBatchGroup, currentBatch)
		}
		currentBatch = &gsharedmemoryproto.Batch{
			Id: stdatomic.AddInt64(&c.uniqueID, 1),
		}
	}
	if len(currentBatch.Deletes)+len(currentBatch.Puts) > 0 {
		currentBatchGroup = append(currentBatchGroup, currentBatch)
	}
	if len(currentBatchGroup) > 0 {
		batchGroups = append(batchGroups, currentBatchGroup)
	}
	return batchGroups, nil
}

type filteredBatch struct {
	writes  map[string][]byte
	deletes map[string]struct{}
}

func (b *filteredBatch) Put(key []byte, value []byte) error {
	keyStr := string(key)
	delete(b.deletes, keyStr)
	b.writes[keyStr] = value
	return nil
}

func (b *filteredBatch) Delete(key []byte) error {
	keyStr := string(key)
	delete(b.writes, keyStr)
	b.deletes[keyStr] = struct{}{}
	return nil
}

func (b *filteredBatch) PutRequests() []*gsharedmemoryproto.BatchPut {
	reqs := make([]*gsharedmemoryproto.BatchPut, 0, len(b.writes))
	for keyStr, value := range b.writes {
		reqs = append(reqs, &gsharedmemoryproto.BatchPut{
			Key:   []byte(keyStr),
			Value: value,
		})
	}
	return reqs
}

func (b *filteredBatch) DeleteRequests() []*gsharedmemoryproto.BatchDelete {
	reqs := make([]*gsharedmemoryproto.BatchDelete, 0, len(b.deletes))
	for keyStr := range b.deletes {
		reqs = append(reqs, &gsharedmemoryproto.BatchDelete{
			Key: []byte(keyStr),
		})
	}
	return reqs
}
