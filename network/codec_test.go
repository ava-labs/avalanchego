// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto/x509"
	"fmt"
	"math"
	"net"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/stretchr/testify/assert"
)

var TestCodec Codec

func TestCodecPackInvalidOp(t *testing.T) {
	_, err := TestCodec.Pack(nil, math.MaxUint8, make(map[Field]interface{}), false)
	assert.Error(t, err)
}

func TestCodecPackMissingField(t *testing.T) {
	_, err := TestCodec.Pack(nil, Get, make(map[Field]interface{}), false)
	assert.Error(t, err)
}

func TestCodecParseInvalidOp(t *testing.T) {
	_, err := TestCodec.Parse([]byte{math.MaxUint8}, true)
	assert.Error(t, err)
}

func TestCodecParseExtraSpace(t *testing.T) {
	_, err := TestCodec.Parse([]byte{byte(GetVersion), 0x00}, true)
	assert.Error(t, err)
}

// Test packing and then parsing messages
// when using a gzip compressor
func TestCodecPackParseGzip(t *testing.T) {
	c := Codec{
		compressor: compression.NewGzipCompressor(),
	}
	id := ids.GenerateTestID()
	cert := &x509.Certificate{}

	msgs := []msg{
		{
			op:     GetVersion,
			fields: map[Field]interface{}{},
		},
		{
			op: Version,
			fields: map[Field]interface{}{
				NetworkID:   uint32(0),
				NodeID:      uint32(1337),
				MyTime:      uint64(time.Now().Unix()),
				IP:          utils.IPDesc{IP: net.IPv4(1, 2, 3, 4)},
				VersionStr:  "v1.2.3",
				VersionTime: uint64(time.Now().Unix()),
				SigBytes:    []byte{'y', 'e', 'e', 't'},
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
			op:     Pong,
			fields: map[Field]interface{}{},
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
			op: MultiPut,
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

	// Test without compression
	for _, m := range msgs {
		packedIntf, err := c.Pack(nil, m.op, m.fields, false)
		assert.NoError(t, err, "failed on operation %s", m.op)

		unpackedIntf, err := c.Parse(packedIntf.Bytes(), true)
		assert.NoError(t, err)

		packed := packedIntf.(*msg)
		unpacked := unpackedIntf.(*msg)

		assert.EqualValues(t, len(packed.fields), len(packed.fields))
		for field := range packed.fields {
			if field == SignedPeers {
				continue // TODO get this to work
			}
			assert.EqualValues(t, packed.fields[field], unpacked.fields[field])
		}
		assert.EqualValues(t, packed.bytes, unpacked.bytes)
	}

	fmt.Println("TEST WITH COMPRESSION")
	// Test with compression
	for i, m := range msgs {
		uncompIntf, err := c.Pack(nil, m.op, m.fields, false)
		assert.NoError(t, err, "failed to pack w/o compression on operation %s", m.op)

		packedIntf, err := c.Pack(nil, m.op, m.fields, true)
		assert.NoError(t, err, "failed to pack w/ compression on operation %s", m.op)

		unpackedIntf, err := c.Parse(packedIntf.Bytes(), true)
		assert.NoError(t, err, "failed to parse w/ compression on operation %s", m.op)

		uncomp := uncompIntf.(*msg)
		packed := packedIntf.(*msg)
		unpacked := unpackedIntf.(*msg)

		assert.EqualValues(t, len(packed.fields), len(unpacked.fields))
		for field := range packed.fields {
			if field == SignedPeers {
				continue // TODO get this to work
			}
			assert.EqualValues(t, packed.fields[field], unpacked.fields[field])
		}
		fmt.Printf("checking %dth message %s op\n", i, m.Op())
		assert.Equal(t, len(uncomp.bytes), len(unpacked.bytes))
		if len(unpacked.bytes) > 2 {
			assert.EqualValues(t, uncomp.bytes[2:], unpacked.bytes[2:], "failed on %dth message %s op", i, m.Op())
		}
	}
}
