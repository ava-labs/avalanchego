// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"math"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/units"
)

func TestCodecPackInvalidOp(t *testing.T) {
	codec, err := NewCodecWithMemoryPool("", prometheus.NewRegistry(), 2*units.MiB, 10*time.Second)
	require.NoError(t, err)

	_, err = codec.Pack(math.MaxUint8, make(map[Field]interface{}), false, false)
	require.Error(t, err)

	_, err = codec.Pack(math.MaxUint8, make(map[Field]interface{}), true, false)
	require.Error(t, err)
}

func TestCodecPackMissingField(t *testing.T) {
	codec, err := NewCodecWithMemoryPool("", prometheus.NewRegistry(), 2*units.MiB, 10*time.Second)
	require.NoError(t, err)

	_, err = codec.Pack(Get, make(map[Field]interface{}), false, false)
	require.Error(t, err)

	_, err = codec.Pack(Get, make(map[Field]interface{}), true, false)
	require.Error(t, err)
}

func TestCodecParseInvalidOp(t *testing.T) {
	codec, err := NewCodecWithMemoryPool("", prometheus.NewRegistry(), 2*units.MiB, 10*time.Second)
	require.NoError(t, err)

	_, err = codec.Parse([]byte{math.MaxUint8}, dummyNodeID, dummyOnFinishedHandling)
	require.Error(t, err)
}

func TestCodecParseExtraSpace(t *testing.T) {
	codec, err := NewCodecWithMemoryPool("", prometheus.NewRegistry(), 2*units.MiB, 10*time.Second)
	require.NoError(t, err)

	_, err = codec.Parse([]byte{byte(Ping), 0x00, 0x00}, dummyNodeID, dummyOnFinishedHandling)
	require.Error(t, err)

	_, err = codec.Parse([]byte{byte(Ping), 0x00, 0x01}, dummyNodeID, dummyOnFinishedHandling)
	require.Error(t, err)
}

func TestDeadlineOverride(t *testing.T) {
	c, err := NewCodecWithMemoryPool("", prometheus.NewRegistry(), 2*units.MiB, 10*time.Second)
	require.NoError(t, err)

	id := ids.GenerateTestID()
	m := inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op: PushQuery,
		},
		fields: map[Field]interface{}{
			ChainID:        id[:],
			RequestID:      uint32(1337),
			Deadline:       uint64(time.Now().Add(1337 * time.Hour).Unix()),
			ContainerID:    id[:],
			ContainerBytes: make([]byte, 1024),
		},
	}

	packedIntf, err := c.Pack(m.op, m.fields, m.op.Compressible(), false)
	require.NoError(t, err, "failed to pack on operation %s", m.op)

	unpackedIntf, err := c.Parse(packedIntf.Bytes(), dummyNodeID, dummyOnFinishedHandling)
	require.NoError(t, err, "failed to parse w/ compression on operation %s", m.op)

	unpacked := unpackedIntf.(*inboundMessageWithPacker)
	require.NotEqual(t, unpacked.ExpirationTime(), time.Now().Add(1337*time.Hour))
	require.True(t, time.Since(unpacked.ExpirationTime()) <= 10*time.Second)
}

// Test packing and then parsing messages
// when using a gzip compressor
func TestCodecPackParseGzip(t *testing.T) {
	c, err := NewCodecWithMemoryPool("", prometheus.DefaultRegisterer, 2*units.MiB, 10*time.Second)
	require.NoError(t, err)
	id := ids.GenerateTestID()

	tlsCert, err := staking.NewTLSCert()
	require.NoError(t, err)
	cert := tlsCert.Leaf

	msgs := []inboundMessageWithPacker{
		{
			inboundMessage: inboundMessage{
				op: Version,
			},
			fields: map[Field]interface{}{
				NetworkID:      uint32(0),
				NodeID:         uint32(1337),
				MyTime:         uint64(time.Now().Unix()),
				IP:             ips.IPPort{IP: net.IPv4(1, 2, 3, 4)},
				VersionStr:     "v1.2.3",
				VersionTime:    uint64(time.Now().Unix()),
				SigBytes:       []byte{'y', 'e', 'e', 't'},
				TrackedSubnets: [][]byte{id[:]},
			},
		},
		{
			inboundMessage: inboundMessage{
				op: PeerList,
			},
			fields: map[Field]interface{}{
				Peers: []ips.ClaimedIPPort{
					{
						Cert:      cert,
						IPPort:    ips.IPPort{IP: net.IPv4(1, 2, 3, 4)},
						Timestamp: uint64(time.Now().Unix()),
						Signature: make([]byte, 65),
					},
				},
			},
		},
		{
			inboundMessage: inboundMessage{
				op: Ping,
			},
			fields: map[Field]interface{}{},
		},
		{
			inboundMessage: inboundMessage{
				op: Pong,
			},
			fields: map[Field]interface{}{
				Uptime: uint8(80),
			},
		},
		{
			inboundMessage: inboundMessage{
				op: GetAcceptedFrontier,
			},
			fields: map[Field]interface{}{
				ChainID:   id[:],
				RequestID: uint32(1337),
				Deadline:  uint64(time.Now().Unix()),
			},
		},
		{
			inboundMessage: inboundMessage{
				op: AcceptedFrontier,
			},
			fields: map[Field]interface{}{
				ChainID:      id[:],
				RequestID:    uint32(1337),
				ContainerIDs: [][]byte{id[:]},
			},
		},
		{
			inboundMessage: inboundMessage{
				op: GetAccepted,
			},
			fields: map[Field]interface{}{
				ChainID:      id[:],
				RequestID:    uint32(1337),
				Deadline:     uint64(time.Now().Unix()),
				ContainerIDs: [][]byte{id[:]},
			},
		},
		{
			inboundMessage: inboundMessage{
				op: Accepted,
			},
			fields: map[Field]interface{}{
				ChainID:      id[:],
				RequestID:    uint32(1337),
				ContainerIDs: [][]byte{id[:]},
			},
		},
		{
			inboundMessage: inboundMessage{
				op: Ancestors,
			},
			fields: map[Field]interface{}{
				ChainID:             id[:],
				RequestID:           uint32(1337),
				MultiContainerBytes: [][]byte{id[:]},
			},
		},
		{
			inboundMessage: inboundMessage{
				op: Get,
			},
			fields: map[Field]interface{}{
				ChainID:     id[:],
				RequestID:   uint32(1337),
				Deadline:    uint64(time.Now().Unix()),
				ContainerID: id[:],
			},
		},
		{
			inboundMessage: inboundMessage{
				op: Put,
			},
			fields: map[Field]interface{}{
				ChainID:        id[:],
				RequestID:      uint32(1337),
				ContainerID:    id[:],
				ContainerBytes: make([]byte, 1024),
			},
		},
		{
			inboundMessage: inboundMessage{
				op: PushQuery,
			},
			fields: map[Field]interface{}{
				ChainID:        id[:],
				RequestID:      uint32(1337),
				Deadline:       uint64(time.Now().Unix()),
				ContainerID:    id[:],
				ContainerBytes: make([]byte, 1024),
			},
		},
		{
			inboundMessage: inboundMessage{
				op: PullQuery,
			},
			fields: map[Field]interface{}{
				ChainID:     id[:],
				RequestID:   uint32(1337),
				Deadline:    uint64(time.Now().Unix()),
				ContainerID: id[:],
			},
		},
		{
			inboundMessage: inboundMessage{
				op: Chits,
			},
			fields: map[Field]interface{}{
				ChainID:      id[:],
				RequestID:    uint32(1337),
				ContainerIDs: [][]byte{id[:]},
			},
		},
	}
	for _, m := range msgs {
		packedIntf, err := c.Pack(m.op, m.fields, m.op.Compressible(), false)
		require.NoError(t, err, "failed to pack on operation %s", m.op)

		unpackedIntf, err := c.Parse(packedIntf.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		require.NoError(t, err, "failed to parse w/ compression on operation %s", m.op)

		unpacked := unpackedIntf.(*inboundMessageWithPacker)

		require.EqualValues(t, len(m.fields), len(unpacked.fields))
	}
}
