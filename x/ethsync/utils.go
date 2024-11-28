// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethsync

import (
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/x/merkledb"
	"github.com/ethereum/go-ethereum/ethdb"
)

// IncrOne increments bytes value by one
func IncrOne(bytes []byte) {
	index := len(bytes) - 1
	for index >= 0 {
		if bytes[index] < 255 {
			bytes[index]++
			break
		} else {
			bytes[index] = 0
			index--
		}
	}
}

var _ ethdb.KeyValueWriter = (*proof)(nil)

type proof []ProofNode

func (p *proof) Put(k, v []byte) error {
	*p = append(*p, ProofNode{
		Key:         merkledb.ToKey(k),
		ValueOrHash: maybe.Some(v),
	})
	return nil
}

func (*proof) Delete([]byte) error {
	panic("should not be called")
}
