// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gsharedmemory

import (
	"github.com/ava-labs/avalanchego/utils/set"

	sharedmemorypb "github.com/ava-labs/avalanchego/proto/pb/sharedmemory"
)

type filteredBatch struct {
	writes  map[string][]byte
	deletes set.Set[string]
}

func (b *filteredBatch) Put(key []byte, value []byte) error {
	keyStr := string(key)
	b.deletes.Remove(keyStr)
	b.writes[keyStr] = value
	return nil
}

func (b *filteredBatch) Delete(key []byte) error {
	keyStr := string(key)
	delete(b.writes, keyStr)
	b.deletes.Add(keyStr)
	return nil
}

func (b *filteredBatch) PutRequests() []*sharedmemorypb.BatchPut {
	reqs := make([]*sharedmemorypb.BatchPut, len(b.writes))
	i := 0
	for keyStr, value := range b.writes {
		reqs[i] = &sharedmemorypb.BatchPut{
			Key:   []byte(keyStr),
			Value: value,
		}
		i++
	}
	return reqs
}

func (b *filteredBatch) DeleteRequests() []*sharedmemorypb.BatchDelete {
	reqs := make([]*sharedmemorypb.BatchDelete, len(b.deletes))
	i := 0
	for keyStr := range b.deletes {
		reqs[i] = &sharedmemorypb.BatchDelete{
			Key: []byte(keyStr),
		}
		i++
	}
	return reqs
}
