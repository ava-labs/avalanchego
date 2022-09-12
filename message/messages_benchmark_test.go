// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"net"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

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
//	$ benchcmp /tmp/cpu.before.txt /tmp/cpu.after.txt
//	$ benchstat -alpha 0.03 -geomean /tmp/cpu.before.txt /tmp/cpu.after.txt
//
//	$ go test -run=NONE -bench=BenchmarkMarshalVersion -benchmem > /tmp/mem.before.txt
//	$ USE_PROTO=true go test -run=NONE -bench=BenchmarkMarshalVersion -benchmem > /tmp/mem.after.txt
//	$ benchcmp /tmp/mem.before.txt /tmp/mem.after.txt
//	$ benchstat -alpha 0.03 -geomean /tmp/mem.before.txt /tmp/mem.after.txt
func BenchmarkMarshalVersion(b *testing.B) {
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
	if err != nil {
		b.Fatal(err)
	}
	packerMsg, err := packerCodec.Pack(inboundMsg.op, inboundMsg.fields, inboundMsg.op.Compressible(), false)
	if err != nil {
		b.Fatal(err)
	}
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

	useProto := os.Getenv("USE_PROTO") == "true"
	b.Logf("marshaling packer %d-byte, proto %d-byte (use proto %v)", packerMsgN, protoMsgN, useProto)

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if !useProto {
			_, err := packerCodec.Pack(inboundMsg.op, inboundMsg.fields, inboundMsg.op.Compressible(), false)
			if err != nil {
				b.Fatal(err)
			}
			continue
		}

		_, err := proto.Marshal(&protoMsg)
		if err != nil {
			b.Fatal(err)
		}
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
//	$ benchcmp /tmp/cpu.before.txt /tmp/cpu.after.txt
//	$ benchstat -alpha 0.03 -geomean /tmp/cpu.before.txt /tmp/cpu.after.txt
//
//	$ go test -run=NONE -bench=BenchmarkUnmarshalVersion -benchmem > /tmp/mem.before.txt
//	$ USE_PROTO=true go test -run=NONE -bench=BenchmarkUnmarshalVersion -benchmem > /tmp/mem.after.txt
//	$ benchcmp /tmp/mem.before.txt /tmp/mem.after.txt
//	$ benchstat -alpha 0.03 -geomean /tmp/mem.before.txt /tmp/mem.after.txt
func BenchmarkUnmarshalVersion(b *testing.B) {
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
	if err != nil {
		b.Fatal(err)
	}

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
	if err != nil {
		b.Fatal(err)
	}

	useProto := os.Getenv("USE_PROTO") == "true"
	if !useProto {
		msgInf, err := packerCodec.Pack(inboundMsg.op, inboundMsg.fields, inboundMsg.op.Compressible(), false)
		if err != nil {
			b.Fatal(err)
		}
		rawMsg = msgInf.Bytes()
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if !useProto {
			_, err := packerCodec.Parse(rawMsg, dummyNodeID, dummyOnFinishedHandling)
			if err != nil {
				b.Fatal(err)
			}
			continue
		}

		var protoMsg p2ppb.Message
		if err := proto.Unmarshal(rawMsg, &protoMsg); err != nil {
			b.Fatal(err)
		}
	}
}
