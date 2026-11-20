// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wal

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteRecord(t *testing.T) {
	require := require.New(t)

	r := []byte{3, 4, 5}
	var buff bytes.Buffer
	require.NoError(writeRecord(&buff, r))
	require.Equal(
		[]byte{
			0, 0, 0, 3, // Length of the payload
			3, 4, 5, // Payload
			0x8f, 0x9, 0x7f, 0xad, 0x19, 0xf9, 0xac, 0x8f, // CRC
		},
		buff.Bytes(),
	)
}

// TestWriteRecordRejectsOversizedPayload ensures a payload whose length does not fit in the
// 32 bit length field is rejected before anything is written.
func TestWriteRecordRejectsOversizedPayload(t *testing.T) {
	payload := make([]byte, math.MaxUint32+1)

	var buff bytes.Buffer
	require.ErrorIs(t, writeRecord(&buff, payload), ErrPayloadTooLarge)
	require.Zero(t, buff.Len())
}

func TestReadRecord(t *testing.T) {
	require := require.New(t)

	r := []byte{3, 4, 5}
	var buff bytes.Buffer
	require.NoError(writeRecord(&buff, r))

	validBuff := bytes.NewBuffer(buff.Bytes())
	readPayload, _, err := readRecord(validBuff, math.MaxUint32)
	require.NoError(err)
	require.Equal(r, readPayload)

	buffBytes := buff.Bytes()
	buffBytes[len(buffBytes)-1]++ // Corrupt the CRC of the buffer
	invalidBuff := bytes.NewBuffer(buffBytes)
	_, _, err = readRecord(invalidBuff, math.MaxUint32)
	require.ErrorIs(err, ErrInvalidCRC)
}

// TestReadRecordRejectsOversizedPayload ensures a record declaring a payload that cannot
// fit in the bytes that remain is rejected. A declared length near MaxUint32 used to wrap
// around when the checksum length was added to it, sizing the allocation far below the
// declared length, so slicing the payload back out of it panicked.
func TestReadRecordRejectsOversizedPayload(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		payloadLen uint32
		maxSize    int64
	}{
		{
			name:       "declared length wraps when the checksum length is added",
			payloadLen: math.MaxUint32,
			maxSize:    math.MaxUint32,
		},
		{
			name:       "declared length exceeds the bytes that remain",
			payloadLen: 4096,
			maxSize:    64,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			buff := make([]byte, recordSizeLen+recordChecksumLen+8)
			binary.BigEndian.PutUint32(buff, testCase.payloadLen)

			payload, n, err := readRecord(bytes.NewReader(buff), testCase.maxSize)
			require.ErrorContains(t, err, "bytes remain")
			require.Nil(t, payload)
			require.Zero(t, n)
		})
	}
}

func FuzzRecord(f *testing.F) {
	f.Fuzz(func(t *testing.T, payload []byte, badCRCVal uint64) {
		require := require.New(t)

		var buff bytes.Buffer
		require.NoError(writeRecord(&buff, payload))

		validBuff := bytes.NewBuffer(buff.Bytes())
		readPayload, n, err := readRecord(validBuff, math.MaxUint32)
		require.NoError(err)
		require.Equal(payload, readPayload)
		require.Equal(recordSizeLen+len(payload)+recordChecksumLen, int(n))

		recordBytes := buff.Bytes()
		crc := recordBytes[len(recordBytes)-recordChecksumLen:]

		badCRC := make([]byte, recordChecksumLen)
		binary.BigEndian.PutUint64(badCRC, badCRCVal)
		if bytes.Equal(crc, badCRC) {
			return
		}

		// Corrupt the CRC of the buffer
		copy(crc, badCRC)

		invalidBuff := bytes.NewBuffer(buff.Bytes())
		_, _, err = readRecord(invalidBuff, math.MaxUint32)
		require.ErrorIs(err, ErrInvalidCRC)
	})
}

func BenchmarkWriteRecord(b *testing.B) {
	payload := []byte{3, 4, 5}
	for range b.N {
		_ = writeRecord(io.Discard, payload)
	}
}

func BenchmarkReadRecord(b *testing.B) {
	payload := []byte{3, 4, 5}
	var originalBuffer bytes.Buffer
	require.NoError(b, writeRecord(&originalBuffer, payload))

	var loopBuffer bytes.Buffer
	for range b.N {
		loopBuffer = originalBuffer // reset the buffer
		_, _, _ = readRecord(&loopBuffer, math.MaxUint32)
	}
}
