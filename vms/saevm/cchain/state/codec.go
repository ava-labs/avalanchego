// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"

	chainsatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

// codecVersion must equal the version used by the old coreth atomic codec
// (graft/coreth/plugin/evm/atomic/codec.go) so that values written to the
// atomic trie are byte-identical to pre-migration entries.
const codecVersion uint16 = 0

var atomicRequestsCodec codec.Manager

func init() {
	atomicRequestsCodec = codec.NewDefaultManager()
	if err := atomicRequestsCodec.RegisterCodec(codecVersion, linearcodec.NewDefault()); err != nil {
		panic(err)
	}
}

// marshalAtomicRequests returns the canonical binary form of r.
//
// chainsatomic.Requests and chainsatomic.Element contain only [][]byte and
// concrete struct pointers, so the encoding depends only on codecVersion and
// not on which types are registered with the codec. This produces the same
// bytes as the old coreth path (atomic_trie.go: a.codec.Marshal(atomic.CodecVersion, requests)).
func marshalAtomicRequests(r *chainsatomic.Requests) ([]byte, error) {
	return atomicRequestsCodec.Marshal(codecVersion, r)
}
