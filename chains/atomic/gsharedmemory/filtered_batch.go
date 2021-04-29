// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gsharedmemory

import (
	"github.com/ava-labs/avalanchego/chains/atomic/gsharedmemory/gsharedmemoryproto"
)

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
