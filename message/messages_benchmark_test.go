// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"net"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/units"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
)

// Benchmarks marshal-ing "Version" message.
//
// e.g.,
//
//	$ go install -v golang.org/x/tools/cmd/benchcmp@latest
//	$ go install -v golang.org/x/perf/cmd/benchstat@latest
//
//	$ go test -run=NONE -bench=BenchmarkMarshalVersion > /tmp/cpu.before.txt
//	$ USE_PROTO=true go test -run=NONE -bench=BenchmarkMarshalVersion > /tmp/cpu.after.txt
//	$ USE_PROTO=true USE_PROTO_BUILDER=true go test -run=NONE -bench=BenchmarkMarshalVersion > /tmp/cpu.after.txt
//	$ benchcmp /tmp/cpu.before.txt /tmp/cpu.after.txt
//	$ benchstat -alpha 0.03 -geomean /tmp/cpu.before.txt /tmp/cpu.after.txt
//
//	$ go test -run=NONE -bench=BenchmarkMarshalVersion -benchmem > /tmp/mem.before.txt
//	$ USE_PROTO=true go test -run=NONE -bench=BenchmarkMarshalVersion -benchmem > /tmp/mem.after.txt
//	$ USE_PROTO=true USE_PROTO_BUILDER=true go test -run=NONE -bench=BenchmarkMarshalVersion -benchmem > /tmp/mem.after.txt
//	$ benchcmp /tmp/mem.before.txt /tmp/mem.after.txt
//	$ benchstat -alpha 0.03 -geomean /tmp/mem.before.txt /tmp/mem.after.txt
func BenchmarkMarshalVersion(b *testing.B) {
	require := require.New(b)

	b.StopTimer()

	id := ids.GenerateTestID()

	// version message does not require compression
	// thus no in-place update for proto test cases
	// which makes the benchmarks fairer to proto
	// as there's no need to copy the original test message
	// for each run
	inboundMsg := inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op: Version,
		},
		fields: map[Field]interface{}{
			NetworkID:      uint32(1337),
			NodeID:         uint32(0),
			MyTime:         uint64(time.Now().Unix()),
			IP:             ips.IPPort{IP: net.IPv4(1, 2, 3, 4)},
			VersionStr:     "v1.2.3",
			VersionTime:    uint64(time.Now().Unix()),
			SigBytes:       []byte{'y', 'e', 'e', 't'},
			TrackedSubnets: [][]byte{id[:]},
		},
	}
	packerCodec, err := NewCodecWithMemoryPool("", prometheus.NewRegistry(), 2*units.MiB, 10*time.Second)
	require.NoError(err)

	packerMsg, err := packerCodec.Pack(inboundMsg.op, inboundMsg.fields, inboundMsg.op.Compressible(), false)
	require.NoError(err)

	packerMsgN := len(packerMsg.Bytes())

	protoMsg := p2ppb.Message{
		Message: &p2ppb.Message_Version{
			Version: &p2ppb.Version{
				NetworkId:      uint32(1337),
				MyTime:         uint64(time.Now().Unix()),
				IpAddr:         []byte(net.IPv4(1, 2, 3, 4).To16()),
				IpPort:         0,
				MyVersion:      "v1.2.3",
				MyVersionTime:  uint64(time.Now().Unix()),
				Sig:            []byte{'y', 'e', 'e', 't'},
				TrackedSubnets: [][]byte{id[:]},
			},
		},
	}
	protoMsgN := proto.Size(&protoMsg)

	useProto := os.Getenv("USE_PROTO") != ""
	useProtoBuilder := os.Getenv("USE_PROTO_BUILDER") != ""

	protoCodec, err := newMsgBuilderProtobuf("", prometheus.NewRegistry(), 2*units.MiB, 10*time.Second)
	require.NoError(err)

	b.Logf("marshaling packer %d-byte, proto %d-byte (use proto %v, use proto builder %v)", packerMsgN, protoMsgN, useProto, useProtoBuilder)

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if !useProto {
			// version does not compress message
			_, err := packerCodec.Pack(inboundMsg.op, inboundMsg.fields, false, false)
			require.NoError(err)
			continue
		}

		if useProtoBuilder {
			_, err = protoCodec.createOutbound(inboundMsg.op, &protoMsg, false, false)
		} else {
			_, err = proto.Marshal(&protoMsg)
		}
		require.NoError(err)
	}
}

// Benchmarks unmarshal-ing "Version" message.
//
// e.g.,
//
//	$ go install -v golang.org/x/tools/cmd/benchcmp@latest
//	$ go install -v golang.org/x/perf/cmd/benchstat@latest
//
//	$ go test -run=NONE -bench=BenchmarkUnmarshalVersion > /tmp/cpu.before.txt
//	$ USE_PROTO=true go test -run=NONE -bench=BenchmarkUnmarshalVersion > /tmp/cpu.after.txt
//	$ USE_PROTO=true USE_PROTO_BUILDER=true go test -run=NONE -bench=BenchmarkUnmarshalVersion > /tmp/cpu.after.txt
//	$ benchcmp /tmp/cpu.before.txt /tmp/cpu.after.txt
//	$ benchstat -alpha 0.03 -geomean /tmp/cpu.before.txt /tmp/cpu.after.txt
//
//	$ go test -run=NONE -bench=BenchmarkUnmarshalVersion -benchmem > /tmp/mem.before.txt
//	$ USE_PROTO=true go test -run=NONE -bench=BenchmarkUnmarshalVersion -benchmem > /tmp/mem.after.txt
//	$ USE_PROTO=true USE_PROTO_BUILDER=true go test -run=NONE -bench=BenchmarkUnmarshalVersion -benchmem > /tmp/mem.after.txt
//	$ benchcmp /tmp/mem.before.txt /tmp/mem.after.txt
//	$ benchstat -alpha 0.03 -geomean /tmp/mem.before.txt /tmp/mem.after.txt
func BenchmarkUnmarshalVersion(b *testing.B) {
	require := require.New(b)

	b.StopTimer()

	id := ids.GenerateTestID()
	inboundMsg := inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op: Version,
		},
		fields: map[Field]interface{}{
			NetworkID:      uint32(1337),
			NodeID:         uint32(0),
			MyTime:         uint64(time.Now().Unix()),
			IP:             ips.IPPort{IP: net.IPv4(1, 2, 3, 4)},
			VersionStr:     "v1.2.3",
			VersionTime:    uint64(time.Now().Unix()),
			SigBytes:       []byte{'y', 'e', 'e', 't'},
			TrackedSubnets: [][]byte{id[:]},
		},
	}
	packerCodec, err := NewCodecWithMemoryPool("", prometheus.NewRegistry(), 2*units.MiB, 10*time.Second)
	require.NoError(err)

	protoMsg := p2ppb.Message{
		Message: &p2ppb.Message_Version{
			Version: &p2ppb.Version{
				NetworkId:      uint32(1337),
				MyTime:         uint64(time.Now().Unix()),
				IpAddr:         []byte(net.IPv4(1, 2, 3, 4).To16()),
				IpPort:         0,
				MyVersion:      "v1.2.3",
				MyVersionTime:  uint64(time.Now().Unix()),
				Sig:            []byte{'y', 'e', 'e', 't'},
				TrackedSubnets: [][]byte{id[:]},
			},
		},
	}

	rawMsg, err := proto.Marshal(&protoMsg)
	require.NoError(err)

	useProto := os.Getenv("USE_PROTO") != ""
	if !useProto {
		msgInf, err := packerCodec.Pack(inboundMsg.op, inboundMsg.fields, inboundMsg.op.Compressible(), false)
		require.NoError(err)
		rawMsg = msgInf.Bytes()
	}

	useProtoBuilder := os.Getenv("USE_PROTO_BUILDER") != ""
	protoCodec, err := newMsgBuilderProtobuf("", prometheus.NewRegistry(), 2*units.MiB, 10*time.Second)
	require.NoError(err)

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if !useProto {
			_, err := packerCodec.Parse(rawMsg, dummyNodeID, dummyOnFinishedHandling)
			require.NoError(err)
			continue
		}

		if useProtoBuilder {
			_, err = protoCodec.parseInbound(rawMsg, dummyNodeID, dummyOnFinishedHandling)
		} else {
			var protoMsg p2ppb.Message
			err = proto.Unmarshal(rawMsg, &protoMsg)
		}
		require.NoError(err)
	}
}
