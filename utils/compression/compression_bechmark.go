// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"testing"

	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"google.golang.org/protobuf/proto"
)

var msgs = [][]byte{}

func init() {
	pingMsg := p2p.Message{
		Message: &p2p.Message_Ping{
			Ping: &p2p.Ping{},
		},
	}
	pingMsgBytes, err := proto.Marshal(&pingMsg)
	if err != nil {
		panic(err)
	}
	msgs = append(msgs, pingMsgBytes)
}

func BenchmarkCompressor(b *testing.B, compressor Compressor, msg []byte) {
	for i := 0; i < b.N; i++ {
		compressed, err := compressor.Compress(msg)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := compressor.Decompress(compressed); err != nil {
			b.Fatal(err)
		}
	}
}
