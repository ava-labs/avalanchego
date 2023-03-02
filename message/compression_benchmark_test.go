// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"math"
	"net"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/prometheus/client_golang/prometheus"
)

// TODO only use messages that are actually compressed on the wire
var msgs = map[string][]byte{}

func init() {
	innerBuilder, err := newMsgBuilder("", prometheus.NewRegistry(), 10*time.Second)
	if err != nil {
		panic(err)
	}
	builder := newOutboundBuilder(false, innerBuilder) // Note compression disabled

	pingMsg, err := builder.Ping()
	if err != nil {
		panic(err)
	}
	msgs["ping"] = pingMsg.Bytes()

	pongMsg, err := builder.Pong(100, []*p2p.SubnetUptime{
		{
			SubnetId: utils.RandomBytes(hashing.HashLen),
			Uptime:   3,
		},
	})
	if err != nil {
		panic(err)
	}
	msgs["pong"] = pongMsg.Bytes()

	versionMsg, err := builder.Version(
		1,
		uint64(time.Now().Unix()),
		ips.IPPort{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: 9651,
		},
		"v1.0.0",
		uint64(time.Now().Unix()),
		utils.RandomBytes(256),
		[]ids.ID{ids.GenerateTestID()},
	)
	if err != nil {
		panic(err)
	}
	msgs["version"] = versionMsg.Bytes()

	chitsMsg, err := builder.Chits(
		ids.GenerateTestID(),
		12341234,
		[]ids.ID{ids.GenerateTestID()},
		[]ids.ID{ids.GenerateTestID()},
		p2p.EngineType_ENGINE_TYPE_SNOWMAN,
	)
	if err != nil {
		panic(err)
	}
	msgs["chits"] = chitsMsg.Bytes()

	peerListAckMsg, err := builder.PeerListAck([]*p2p.PeerAck{
		{
			TxId:      utils.RandomBytes(hashing.HashLen),
			Timestamp: uint64(time.Now().Unix()),
		},
		{
			TxId:      utils.RandomBytes(hashing.HashLen),
			Timestamp: uint64(time.Now().Unix()),
		},
		{
			TxId:      utils.RandomBytes(hashing.HashLen),
			Timestamp: uint64(time.Now().Unix()),
		},
	})
	if err != nil {
		panic(err)
	}
	msgs["peerListAck"] = peerListAckMsg.Bytes()
}

func BenchmarkCompressor(b *testing.B) {
	zstdCompressor := compression.NewZstdCompressor()
	gzipCompressor, err := compression.NewGzipCompressor(math.MaxUint32)
	if err != nil {
		b.Fatal(err)
	}

	compressors := map[string]compression.Compressor{
		"zstd": zstdCompressor,
		"gzip": gzipCompressor,
	}

	for compressorName, compressor := range compressors {
		for msgName, msg := range msgs {
			b.Run(compressorName+" "+msgName, func(b *testing.B) {
				var bytesSaved int
				for i := 0; i < b.N; i++ {
					compressed, err := compressor.Compress(msg)
					if err != nil {
						b.Fatal(err)
					}
					// if _, err := compressor.Decompress(compressed); err != nil {
					// 	b.Fatal(err)
					// }
					bytesSaved += len(msg) - len(compressed)
				}
				b.ReportMetric(float64(bytesSaved)/float64(b.N), "bytes_saved/op")
			})
		}
	}
}
