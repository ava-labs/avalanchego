// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gsharedmemory

import (
	sharedmemorypb "github.com/chain4travel/caminogo/proto/pb/sharedmemory"
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
