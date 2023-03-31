// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
)

var (
	dummyNodeID             = ids.EmptyNodeID
	dummyOnFinishedHandling = func() {}
)

// Benchmarks marshal-ing "Version" message.
//
// e.g.,
//
//	$ go install -v golang.org/x/tools/cmd/benchcmp@latest
//	$ go install -v golang.org/x/perf/cmd/benchstat@latest
//
//	$ go test -run=NONE -bench=BenchmarkMarshalVersion > /tmp/cpu.before.txt
//	$ USE_BUILDER=true go test -run=NONE -bench=BenchmarkMarshalVersion > /tmp/cpu.after.txt
//	$ benchcmp /tmp/cpu.before.txt /tmp/cpu.after.txt
//	$ benchstat -alpha 0.03 -geomean /tmp/cpu.before.txt /tmp/cpu.after.txt
//
//	$ go test -run=NONE -bench=BenchmarkMarshalVersion -benchmem > /tmp/mem.before.txt
//	$ USE_BUILDER=true go test -run=NONE -bench=BenchmarkMarshalVersion -benchmem > /tmp/mem.after.txt
//	$ benchcmp /tmp/mem.before.txt /tmp/mem.after.txt
//	$ benchstat -alpha 0.03 -geomean /tmp/mem.before.txt /tmp/mem.after.txt
func BenchmarkMarshalVersion(b *testing.B) {
	require := require.New(b)

	id := ids.GenerateTestID()
	msg := p2p.Message{
		Message: &p2p.Message_Version{
			Version: &p2p.Version{
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
	msgLen := proto.Size(&msg)

	useBuilder := os.Getenv("USE_BUILDER") != ""

	codec, err := newMsgBuilder("", prometheus.NewRegistry(), 10*time.Second)
	require.NoError(err)

	b.Logf("proto length %d-byte (use builder %v)", msgLen, useBuilder)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if useBuilder {
			_, err = codec.createOutbound(&msg, false, false)
		} else {
			_, err = proto.Marshal(&msg)
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
//	$ USE_BUILDER=true go test -run=NONE -bench=BenchmarkUnmarshalVersion > /tmp/cpu.after.txt
//	$ benchcmp /tmp/cpu.before.txt /tmp/cpu.after.txt
//	$ benchstat -alpha 0.03 -geomean /tmp/cpu.before.txt /tmp/cpu.after.txt
//
//	$ go test -run=NONE -bench=BenchmarkUnmarshalVersion -benchmem > /tmp/mem.before.txt
//	$ USE_BUILDER=true go test -run=NONE -bench=BenchmarkUnmarshalVersion -benchmem > /tmp/mem.after.txt
//	$ benchcmp /tmp/mem.before.txt /tmp/mem.after.txt
//	$ benchstat -alpha 0.03 -geomean /tmp/mem.before.txt /tmp/mem.after.txt
func BenchmarkUnmarshalVersion(b *testing.B) {
	require := require.New(b)

	b.StopTimer()

	id := ids.GenerateTestID()
	msg := p2p.Message{
		Message: &p2p.Message_Version{
			Version: &p2p.Version{
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

	rawMsg, err := proto.Marshal(&msg)
	require.NoError(err)

	useBuilder := os.Getenv("USE_BUILDER") != ""
	codec, err := newMsgBuilder("", prometheus.NewRegistry(), 10*time.Second)
	require.NoError(err)

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if useBuilder {
			_, err = codec.parseInbound(rawMsg, dummyNodeID, dummyOnFinishedHandling)
		} else {
			var msg p2p.Message
			err = proto.Unmarshal(rawMsg, &msg)
		}
		require.NoError(err)
	}
}
