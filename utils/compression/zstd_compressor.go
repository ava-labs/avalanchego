// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"errors"
	"fmt"
	"math"

	"github.com/klauspost/compress/zstd"
)

var (
	_ Compressor = (*zstdCompressor)(nil)

	ErrInvalidMaxSizeCompressor = errors.New("invalid compressor max size")
	ErrDecompressedMsgTooLarge  = errors.New("decompressed msg too large")
	ErrMsgTooLarge              = errors.New("msg too large to be compressed")
)

func NewZstdCompressor(maxSize int64) (Compressor, error) {
	return NewZstdCompressorWithLevel(maxSize, zstd.SpeedDefault)
}

func NewZstdCompressorWithLevel(maxSize int64, level zstd.EncoderLevel) (Compressor, error) {
	if maxSize <= 0 || maxSize == math.MaxInt64 {
		return nil, ErrInvalidMaxSizeCompressor
	}

	encoder, err := zstd.NewWriter(
		nil,
		zstd.WithEncoderLevel(level),
		// Roughly halves the encoder's retained memory, with no measurable
		// cost to encoding speed at our message sizes.
		zstd.WithLowerEncoderMem(true),
	)
	if err != nil {
		return nil, err
	}

	decoder, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(uint64(maxSize)))
	if err != nil {
		return nil, err
	}

	return &zstdCompressor{
		maxSize: maxSize,
		encoder: encoder,
		decoder: decoder,
	}, nil
}

type zstdCompressor struct {
	maxSize int64
	encoder *zstd.Encoder
	decoder *zstd.Decoder
}

func (z *zstdCompressor) Compress(msg []byte) ([]byte, error) {
	if int64(len(msg)) > z.maxSize {
		return nil, fmt.Errorf("%w: (%d) > (%d)", ErrMsgTooLarge, len(msg), z.maxSize)
	}
	return z.encoder.EncodeAll(msg, nil), nil
}

func (z *zstdCompressor) Decompress(msg []byte) ([]byte, error) {
	decompressed, err := z.decoder.DecodeAll(msg, nil)
	switch {
	// ErrWindowSizeExceeded is reported instead of ErrDecoderSizeExceeded when
	// the frame header declares a window larger than maxSize. Either way the
	// frame is rejected because it would decompress to, or buffer, more than
	// maxSize bytes.
	case errors.Is(err, zstd.ErrDecoderSizeExceeded), errors.Is(err, zstd.ErrWindowSizeExceeded):
		return nil, fmt.Errorf("%w: > (%d)", ErrDecompressedMsgTooLarge, z.maxSize)
	case err != nil:
		return nil, err
	}
	return decompressed, nil
}
