// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc64"
	"io"
	"math"
)

const (
	recordSizeLen     = 4
	recordChecksumLen = 8
)

var (
	ErrInvalidCRC      = errors.New("invalid CRC checksum")
	ErrPayloadTooLarge = errors.New("payload too large for record length field")
)

// writeRecord writes a length-prefixed and check-summed record to the writer.
func writeRecord(w io.Writer, payload []byte) error {
	if uint64(len(payload)) > math.MaxUint32 {
		return fmt.Errorf("%w: %d bytes", ErrPayloadTooLarge, len(payload))
	}

	checksumIndex := recordSizeLen + len(payload)
	buff := make([]byte, checksumIndex+recordChecksumLen)
	crc := crc64.New(crc64.MakeTable(crc64.ECMA))

	binary.BigEndian.PutUint32(buff, uint32(len(payload)))
	copy(buff[recordSizeLen:], payload)
	if _, err := crc.Write(buff[:checksumIndex]); err != nil {
		return fmt.Errorf("CRC checksum failed: %w", err)
	}

	buff = crc.Sum(buff[:checksumIndex])
	_, err := w.Write(buff)
	return err
}

// readRecord reads a length-prefixed and check-summed record from the reader.
// If the record is read correctly, the number of bytes read is returned.
func readRecord(r io.Reader, maxSize int64) ([]byte, int64, error) {
	const tempSpace = max(recordSizeLen, recordChecksumLen)
	buff := make([]byte, tempSpace)
	crc := crc64.New(crc64.MakeTable(crc64.ECMA))

	sizeBuff := buff[:recordSizeLen]
	if _, err := io.ReadFull(r, sizeBuff); err != nil {
		return nil, 0, err
	}
	if _, err := crc.Write(sizeBuff); err != nil {
		return nil, 0, fmt.Errorf("CRC checksum failed: %w", err)
	}

	payloadLen := binary.BigEndian.Uint32(sizeBuff)
	// Widened to 64 bits so that a payload length near MaxUint32 cannot wrap around
	// and understate the size of the record or of the allocation below.
	recordLen := int64(recordSizeLen) + int64(payloadLen) + int64(recordChecksumLen)
	if recordLen > maxSize {
		return nil, 0, fmt.Errorf("record indicates payload is %d bytes long, but only %d bytes remain", payloadLen, maxSize)
	}

	payloadAndChecksum := make([]byte, int64(payloadLen)+int64(recordChecksumLen))
	if _, err := io.ReadFull(r, payloadAndChecksum); err != nil {
		return nil, 0, err
	}
	payload := payloadAndChecksum[:payloadLen]
	if _, err := crc.Write(payload); err != nil {
		return nil, 0, fmt.Errorf("CRC checksum failed: %w", err)
	}

	checksum := payloadAndChecksum[payloadLen:]
	expectedChecksum := crc.Sum(buff[:0])
	if !bytes.Equal(checksum, expectedChecksum) {
		return nil, 0, ErrInvalidCRC
	}
	return payload, recordLen, nil
}
