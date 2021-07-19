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
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	maxBatchSize = 64 * units.KiB // 64 KiB

	// baseElementSize is an approximation of the protobuf encoding overhead per
	// element
	baseElementSize = 8 // bytes
)

var _ atomic.SharedMemory = &Client{}

// Client is atomic.SharedMemory that talks over RPC.
type Client struct {
	client gsharedmemoryproto.SharedMemoryClient

	uniqueID int64
}

// NewClient returns shared memory connected to remote shared memory
func NewClient(client gsharedmemoryproto.SharedMemoryClient) *Client {
	return &Client{client: client}
}

func (c *Client) Get(peerChainID ids.ID, keys [][]byte) ([][]byte, error) {
	req := &gsharedmemoryproto.GetRequest{
		PeerChainID: peerChainID[:],
		Id:          stdatomic.AddInt64(&c.uniqueID, 1),
		Continues:   true,
	}

	currentSize := 0
	prevIndex := 0
	for i, key := range keys {
		sizeChange := baseElementSize + len(key)
		if newSize := currentSize + sizeChange; newSize > maxBatchSize {
			_, err := c.client.Get(context.Background(), req)
			if err != nil {
				return nil, err
			}

			currentSize = 0
			prevIndex = i
			req.PeerChainID = nil
		}
		currentSize += sizeChange

		req.Keys = keys[prevIndex : i+1]
	}

	req.Continues = false
	resp, err := c.client.Get(context.Background(), req)
	if err != nil {
		return nil, err
	}

	values := resp.Values

	req.PeerChainID = nil
	req.Keys = nil
	for resp.Continues {
		resp, err = c.client.Get(context.Background(), req)
		if err != nil {
			return nil, err
		}
		values = append(values, resp.Values...)
	}
	return values, nil
}

func (c *Client) Indexed(
	peerChainID ids.ID,
	traits [][]byte,
	startTrait,
	startKey []byte,
	limit int,
) (
	[][]byte,
	[]byte,
	[]byte,
	error,
) {
	req := &gsharedmemoryproto.IndexedRequest{
		PeerChainID: peerChainID[:],
		StartTrait:  startTrait,
		StartKey:    startKey,
		Limit:       int32(limit),
		Id:          stdatomic.AddInt64(&c.uniqueID, 1),
		Continues:   true,
	}

	currentSize := 0
	prevIndex := 0
	for i, trait := range traits {
		sizeChange := baseElementSize + len(trait)
		if newSize := currentSize + sizeChange; newSize > maxBatchSize {
			_, err := c.client.Indexed(context.Background(), req)
			if err != nil {
				return nil, nil, nil, err
			}

			currentSize = 0
			prevIndex = i
			req.PeerChainID = nil
			req.StartTrait = nil
			req.StartKey = nil
			req.Limit = 0
		}
		currentSize += sizeChange

		req.Traits = traits[prevIndex : i+1]
	}

	req.Continues = false
	resp, err := c.client.Indexed(context.Background(), req)
	if err != nil {
		return nil, nil, nil, err
	}
	lastTrait := resp.LastTrait
	lastKey := resp.LastKey
	values := resp.Values

	req.PeerChainID = nil
	req.Traits = nil
	req.StartTrait = nil
	req.StartKey = nil
	req.Limit = 0
	for resp.Continues {
		resp, err = c.client.Indexed(context.Background(), req)
		if err != nil {
			return nil, nil, nil, err
		}
		values = append(values, resp.Values...)
	}
	return values, lastTrait, lastKey, nil
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
	if len(currentBatchGroup) > 0 {
		batchGroups = append(batchGroups, currentBatchGroup)
	}
	return batchGroups, nil
}

func (c *Client) RemoveAndPutMultiple(batchChainsAndInputs map[ids.ID]*atomic.Requests, batch ...database.Batch) error {
	req := &gsharedmemoryproto.RemoveAndPutMultipleRequest{
		Continues: true,
		Id:        stdatomic.AddInt64(&c.uniqueID, 1),
	}

	for key, value := range batchChainsAndInputs {
		newPutElements := make([][]*gsharedmemoryproto.Element, 0)
		newRemoveElements := make([][][]byte, 0)

		currentPutSize, currentRemoveSize := 0, 0
		prevPutIndex, prevRemoveIndex := 0, 0
		holderPutIndex, holderRemoveIndex := 0, 0

		for k, v := range value.PutRequests {
			sizePutChange := baseElementSize + len(v.Key) + len(v.Value)
			for _, trait := range v.Traits {
				sizePutChange += len(trait)
			}

			if newSize := sizePutChange + currentPutSize; newSize > maxBatchSize {
				currentPutSize = 0
				formattedElements := make([]*gsharedmemoryproto.Element, 0, len(value.PutRequests[prevPutIndex:k+1]))
				for _, v := range value.PutRequests[prevPutIndex : k+1] {
					formattedElements = append(formattedElements, &gsharedmemoryproto.Element{
						Key:    v.Key,
						Value:  v.Value,
						Traits: v.Traits,
					})
				}
				newPutElements = append(newPutElements, formattedElements)
				prevPutIndex = k + 1
			} else {
				holderPutIndex = k
			}
			currentPutSize += sizePutChange
		}

		if len(value.PutRequests) > 0 && currentPutSize <= maxBatchSize {
			formattedElements := make([]*gsharedmemoryproto.Element, 0, len(value.PutRequests[prevPutIndex:holderPutIndex+1]))
			for _, v := range value.PutRequests[prevPutIndex : holderPutIndex+1] {
				formattedElements = append(formattedElements, &gsharedmemoryproto.Element{
					Key:    v.Key,
					Value:  v.Value,
					Traits: v.Traits,
				})
			}
			newPutElements = append(newPutElements, formattedElements)
		}

		for i, key := range value.RemoveRequests {
			sizeRemoveChange := baseElementSize + len(key)
			if newSize := sizeRemoveChange + currentRemoveSize; newSize > maxBatchSize {
				currentRemoveSize = 0
				newRemoveElements = append(newRemoveElements, value.RemoveRequests[prevRemoveIndex:i+1])
				prevRemoveIndex = i + 1
			} else {
				holderRemoveIndex = i
			}

			currentRemoveSize += sizeRemoveChange
		}
		if len(value.RemoveRequests) > 0 && currentRemoveSize <= maxBatchSize {
			newRemoveElements = append(newRemoveElements, value.RemoveRequests[prevRemoveIndex:holderRemoveIndex+1])
		}

		maxLoop := 0
		if len(newPutElements) < len(newRemoveElements) {
			maxLoop = len(newRemoveElements)
		} else {
			maxLoop = len(newPutElements)
		}

		for i := 0; i < maxLoop; i++ {
			request := &gsharedmemoryproto.RemoveAndPutMultipleRequest{
				Continues: true,
				Id:        req.Id,
			}
			switch {
			case i < len(newRemoveElements) && i < len(newPutElements):
				request.BatchChainsAndInputs = []*gsharedmemoryproto.AtomicRequest{{
					RemoveRequests: newRemoveElements[i],
					PutRequests:    newPutElements[i],
					PeerChainID:    key[:],
				}}
			case i < len(newRemoveElements):
				request.BatchChainsAndInputs = []*gsharedmemoryproto.AtomicRequest{{
					RemoveRequests: newRemoveElements[i],
					PeerChainID:    key[:],
				}}
			case i < len(newPutElements):
				request.BatchChainsAndInputs = []*gsharedmemoryproto.AtomicRequest{{
					PutRequests: newPutElements[i],
					PeerChainID: key[:],
				}}
			}
			if _, err := c.client.RemoveAndPutMultiple(context.Background(), request); err != nil {
				return err
			}
		}
	}
	currentSize := 0
	batchGroups, err := c.makeBatches(batch, currentSize)
	if err != nil {
		return err
	}

	for i, batches := range batchGroups {
		req.Batches = batches
		req.Continues = i < len(batchGroups)-1
		if _, err := c.client.RemoveAndPutMultiple(context.Background(), req); err != nil {
			return err
		}
	}

	if len(batchGroups) == 0 {
		req.Continues = false
		if _, err := c.client.RemoveAndPutMultiple(context.Background(), req); err != nil {
			return err
		}
	}
	return nil
}
