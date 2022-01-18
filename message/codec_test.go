// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"crypto/x509"
	"math"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/units"
)

func TestCodecPackInvalidOp(t *testing.T) {
	codec, err := NewCodecWithMemoryPool("", prometheus.NewRegistry(), 2*units.MiB)
	assert.NoError(t, err)

	_, err = codec.Pack(math.MaxUint8, make(map[Field]interface{}), false)
	assert.Error(t, err)

	_, err = codec.Pack(math.MaxUint8, make(map[Field]interface{}), true)
	assert.Error(t, err)
}

func TestCodecPackMissingField(t *testing.T) {
	codec, err := NewCodecWithMemoryPool("", prometheus.NewRegistry(), 2*units.MiB)
	assert.NoError(t, err)

	_, err = codec.Pack(Get, make(map[Field]interface{}), false)
	assert.Error(t, err)

	_, err = codec.Pack(Get, make(map[Field]interface{}), true)
	assert.Error(t, err)
}

func TestCodecParseInvalidOp(t *testing.T) {
	codec, err := NewCodecWithMemoryPool("", prometheus.NewRegistry(), 2*units.MiB)
	assert.NoError(t, err)

	_, err = codec.Parse([]byte{math.MaxUint8}, dummyNodeID, dummyOnFinishedHandling)
	assert.Error(t, err)
}

func TestCodecParseExtraSpace(t *testing.T) {
	codec, err := NewCodecWithMemoryPool("", prometheus.NewRegistry(), 2*units.MiB)
	assert.NoError(t, err)

	_, err = codec.Parse([]byte{byte(GetVersion), 0x00, 0x00}, dummyNodeID, dummyOnFinishedHandling)
	assert.Error(t, err)

	_, err = codec.Parse([]byte{byte(GetVersion), 0x00, 0x01}, dummyNodeID, dummyOnFinishedHandling)
	assert.Error(t, err)
}

// Test packing and then parsing messages
// when using a gzip compressor
func TestCodecPackParseGzip(t *testing.T) {
	c, err := NewCodecWithMemoryPool("", prometheus.DefaultRegisterer, 2*units.MiB)
	assert.NoError(t, err)
	id := ids.GenerateTestID()
	cert := &x509.Certificate{}

	msgs := []inboundMessage{
		{
			op:     GetVersion,
			fields: map[Field]interface{}{},
		},
		{
			op: Version,
			fields: map[Field]interface{}{
				NetworkID:      uint32(0),
				NodeID:         uint32(1337),
				MyTime:         uint64(time.Now().Unix()),
				IP:             utils.IPDesc{IP: net.IPv4(1, 2, 3, 4)},
				VersionStr:     "v1.2.3",
				VersionTime:    uint64(time.Now().Unix()),
				SigBytes:       []byte{'y', 'e', 'e', 't'},
				TrackedSubnets: [][]byte{id[:]},
			},
		},
		{
			op:     GetPeerList,
			fields: map[Field]interface{}{},
		},
		{
			op: PeerList,
			fields: map[Field]interface{}{
				SignedPeers: []utils.IPCertDesc{
					{
						Cert:      cert,
						IPDesc:    utils.IPDesc{IP: net.IPv4(1, 2, 3, 4)},
						Time:      uint64(time.Now().Unix()),
						Signature: make([]byte, 65),
					},
				},
			},
		},
		{
			op:     Ping,
			fields: map[Field]interface{}{},
		},
		{
			op: Pong,
			fields: map[Field]interface{}{
				Uptime: uint8(80),
			},
		},
		{
			op: GetAcceptedFrontier,
			fields: map[Field]interface{}{
				ChainID:   id[:],
				RequestID: uint32(1337),
				Deadline:  uint64(time.Now().Unix()),
			},
		},
		{
			op: AcceptedFrontier,
			fields: map[Field]interface{}{
				ChainID:      id[:],
				RequestID:    uint32(1337),
				ContainerIDs: [][]byte{id[:]},
			},
		},
		{
			op: GetAccepted,
			fields: map[Field]interface{}{
				ChainID:      id[:],
				RequestID:    uint32(1337),
				Deadline:     uint64(time.Now().Unix()),
				ContainerIDs: [][]byte{id[:]},
			},
		},
		{
			op: Accepted,
			fields: map[Field]interface{}{
				ChainID:      id[:],
				RequestID:    uint32(1337),
				ContainerIDs: [][]byte{id[:]},
			},
		},
		{
			op: Ancestors,
			fields: map[Field]interface{}{
				ChainID:             id[:],
				RequestID:           uint32(1337),
				MultiContainerBytes: [][]byte{id[:]},
			},
		},
		{
			op: Get,
			fields: map[Field]interface{}{
				ChainID:     id[:],
				RequestID:   uint32(1337),
				Deadline:    uint64(time.Now().Unix()),
				ContainerID: id[:],
			},
		},
		{
			op: Put,
			fields: map[Field]interface{}{
				ChainID:        id[:],
				RequestID:      uint32(1337),
				ContainerID:    id[:],
				ContainerBytes: make([]byte, 1024),
			},
		},
		{
			op: PushQuery,
			fields: map[Field]interface{}{
				ChainID:        id[:],
				RequestID:      uint32(1337),
				Deadline:       uint64(time.Now().Unix()),
				ContainerID:    id[:],
				ContainerBytes: make([]byte, 1024),
			},
		},
		{
			op: PullQuery,
			fields: map[Field]interface{}{
				ChainID:     id[:],
				RequestID:   uint32(1337),
				Deadline:    uint64(time.Now().Unix()),
				ContainerID: id[:],
			},
		},
		{
			op: Chits,
			fields: map[Field]interface{}{
				ChainID:      id[:],
				RequestID:    uint32(1337),
				ContainerIDs: [][]byte{id[:]},
			},
		},
	}
	for _, m := range msgs {
		packedIntf, err := c.Pack(m.op, m.fields, m.op.Compressable())
		assert.NoError(t, err, "failed to pack on operation %s", m.op)

		unpackedIntf, err := c.Parse(packedIntf.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		assert.NoError(t, err, "failed to parse w/ compression on operation %s", m.op)

		unpacked := unpackedIntf.(*inboundMessage)

		assert.EqualValues(t, len(m.fields), len(unpacked.fields))
	}
}
