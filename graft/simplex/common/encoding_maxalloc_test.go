// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func sizePrefixedRecord(recordType uint16, size uint32, tail ...byte) []byte {
	b := make([]byte, 6+len(tail))
	binary.BigEndian.PutUint16(b, recordType)
	binary.BigEndian.PutUint32(b[2:], size)
	copy(b[6:], tail)
	return b
}

func TestBlockRecordAllocationLimit(t *testing.T) {
	bh := BlockHeader{ProtocolMetadata: ProtocolMetadata{Version: 1, Round: 7, Seq: 2, Epoch: 3}}

	// Happy path: a well-formed record round-trips.
	rec, err := BlockRecord(bh, []byte{1, 2, 3})
	require.NoError(t, err)

	gotBH, data, err := ParseBlockRecord(rec)
	require.NoError(t, err)
	require.True(t, bh.Equals(&gotBH))
	require.Equal(t, []byte{1, 2, 3}, data)

	// Block data large enough that the total record would exceed the allocation
	// limit: BlockRecord must reject it before allocating the output buffer.
	oversized := make([]byte, maxAllocationSize)
	_, err = BlockRecord(bh, oversized)
	require.ErrorContains(t, err, "block record size exceeds")
}

func TestParseBlockRecordErrors(t *testing.T) {
	bh := BlockHeader{ProtocolMetadata: ProtocolMetadata{Version: 1, Round: 7, Seq: 2, Epoch: 3}}
	valid, err := BlockRecord(bh, []byte{9, 9, 9})
	require.NoError(t, err)
	mdSize := int(binary.BigEndian.Uint32(valid[2:6]))

	// A valid header followed by no block data.
	noData := append([]byte(nil), valid[:6+mdSize]...)

	// Total length of 3: record type present, but fewer than 4 bytes remain for
	// the metadata size field.
	shortSize := make([]byte, 3)
	binary.BigEndian.PutUint16(shortSize, BlockRecordType)

	for _, tc := range []struct {
		name        string
		input       []byte
		errContains string
	}{
		{"valid round-trips", valid, ""},
		{"nil buffer", nil, "at least 2 bytes for record type"},
		{"single byte", []byte{0x00}, "at least 2 bytes for record type"},
		{"wrong record type", sizePrefixedRecord(NotarizationRecordType, 0), "expected record type"},
		{"missing metadata size", shortSize, "at least 4 bytes for metadata size"},
		{"metadata size too large", sizePrefixedRecord(BlockRecordType, maxAllocationSize+1), "metadata size too large"},
		{"metadata size at limit but truncated", sizePrefixedRecord(BlockRecordType, maxAllocationSize), "buffer too small, expected"},
		{"declared size exceeds buffer", sizePrefixedRecord(BlockRecordType, 50, 0x01, 0x02), "buffer too small, expected"},
		{"invalid metadata bytes", sizePrefixedRecord(BlockRecordType, 1, 0x80, 0x00), "failed to deserialize block metadata"},
		{"no block data", noData, "expected block data but gone none"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ParseBlockRecord(tc.input)
			if tc.errContains == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.errContains)
		})
	}
}

func TestBlockRecordRoundAndEpochErrors(t *testing.T) {
	bh := BlockHeader{ProtocolMetadata: ProtocolMetadata{Version: 1, Round: 42, Seq: 2, Epoch: 3}}
	valid, err := BlockRecord(bh, []byte{9})
	require.NoError(t, err)

	for _, tc := range []struct {
		name          string
		input         []byte
		expectedRound uint64
		expectedEpoch uint64
		errContains   string
	}{
		{"valid returns round and epoch", valid, 42, 3, ""},
		{"too short for metadata size", []byte{0, byte(BlockRecordType), 0}, 0, 0, "too short to extract metadata size"},
		{"metadata size too large", sizePrefixedRecord(BlockRecordType, maxAllocationSize+1), 0, 0, "metadata size too large"},
		{"metadata size at limit but truncated", sizePrefixedRecord(BlockRecordType, maxAllocationSize), 0, 0, "too short to extract round"},
		{"declared size exceeds buffer", sizePrefixedRecord(BlockRecordType, 40, 0x01), 0, 0, "too short to extract round"},
		{"invalid metadata bytes", sizePrefixedRecord(BlockRecordType, 1, 0x80), 0, 0, "failed to deserialize block metadata"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			round, epoch, err := walRecordRoundAndEpoch(tc.input)
			if tc.errContains == "" {
				require.NoError(t, err)
				require.Equal(t, tc.expectedRound, round)
				require.Equal(t, tc.expectedEpoch, epoch)
				return
			}
			require.ErrorContains(t, err, tc.errContains)
		})
	}
}

func TestNotarizationQuorumRecordRoundAndEpochErrors(t *testing.T) {
	vote := ToBeSignedVote{BlockHeader{ProtocolMetadata: ProtocolMetadata{Version: 1, Round: 99, Seq: 2, Epoch: 3}}}
	valid := NewQuorumRecord([]byte{1, 2, 3}, vote.Bytes(), NotarizationRecordType)

	for _, tc := range []struct {
		name          string
		input         []byte
		expectedRound uint64
		expectedEpoch uint64
		errContains   string
	}{
		{"valid returns round and epoch", valid, 99, 3, ""},
		{"too short for vote size", []byte{0, byte(NotarizationRecordType), 0}, 0, 0, "too short to extract vote size"},
		{"vote size too large", sizePrefixedRecord(NotarizationRecordType, maxAllocationSize+1), 0, 0, "vote size too large"},
		{"vote size at limit but truncated", sizePrefixedRecord(NotarizationRecordType, maxAllocationSize), 0, 0, "too short to extract vote,"},
		{"declared size exceeds buffer", sizePrefixedRecord(NotarizationRecordType, 40, 0x01), 0, 0, "too short to extract vote,"},
		{"invalid vote bytes", sizePrefixedRecord(NotarizationRecordType, 1, 0x80), 0, 0, "failed to deserialize vote"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			round, epoch, err := walRecordRoundAndEpoch(tc.input)
			if tc.errContains == "" {
				require.NoError(t, err)
				require.Equal(t, tc.expectedRound, round)
				require.Equal(t, tc.expectedEpoch, epoch)
				return
			}
			require.ErrorContains(t, err, tc.errContains)
		})
	}
}
