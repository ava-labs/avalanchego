// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gsharedmemory

import (
	"github.com/ava-labs/avalanchego/api/proto/gsharedmemoryproto"
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
	reqs := make([]*gsharedmemoryproto.BatchPut, len(b.writes))
	i := 0
	for keyStr, value := range b.writes {
		reqs[i] = &gsharedmemoryproto.BatchPut{
			Key:   []byte(keyStr),
			Value: value,
		}
		i++
	}
	return reqs
}

func (b *filteredBatch) DeleteRequests() []*gsharedmemoryproto.BatchDelete {
	reqs := make([]*gsharedmemoryproto.BatchDelete, len(b.deletes))
	i := 0
	for keyStr := range b.deletes {
		reqs[i] = &gsharedmemoryproto.BatchDelete{
			Key: []byte(keyStr),
		}
		i++
	}
	return reqs
}
