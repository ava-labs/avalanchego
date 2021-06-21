// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"bytes"
	"compress/gzip"
	"io"
)

var gzipHeaderBytes = []byte{0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 0, 255}

const compressionThresholdBytes = 128

type Compressor interface {
	Compress([]byte) ([]byte, error)
	Decompress([]byte) ([]byte, error)
	IsCompressed([]byte) bool
	IsCompressable([]byte) bool
}

// gzipCompressor implements Compressor
type gzipCompressor struct {
	writerInitialised bool
	readerInitialised bool

	writeBuffer *bytes.Buffer
	gzipWriter  *gzip.Writer

	bytesReader *bytes.Reader
	gzipReader  *gzip.Reader
}

// Compress [msg] and returns the compressed bytes.
func (g *gzipCompressor) Compress(msg []byte) ([]byte, error) {
	g.resetWriter()
	if _, err := g.gzipWriter.Write(msg); err != nil {
		return msg, err
	}
	if err := g.gzipWriter.Flush(); err != nil {
		return msg, err
	}
	if err := g.gzipWriter.Close(); err != nil {
		return msg, err
	}
	return g.writeBuffer.Bytes(), nil
}

// Decompress decompresses [msg].
func (g *gzipCompressor) Decompress(msg []byte) ([]byte, error) {
	if err := g.resetReader(msg); err != nil {
		return nil, err
	}

	return io.ReadAll(g.gzipReader)
}

// IsCompressed returns whether given bytes are compressed data or not
func (g *gzipCompressor) IsCompressed(msg []byte) bool {
	return len(msg) > 10 && bytes.Equal(msg[:10], gzipHeaderBytes)
}

func (g *gzipCompressor) IsCompressable(msg []byte) bool {
	return len(msg) > compressionThresholdBytes
}

func (g *gzipCompressor) resetWriter() {
	if !g.writerInitialised {
		var buf bytes.Buffer
		g.writeBuffer = &buf
		g.gzipWriter = gzip.NewWriter(g.writeBuffer)
		g.writerInitialised = true
	} else {
		g.writeBuffer.Reset()
		g.gzipWriter.Reset(g.writeBuffer)
	}
}

func (g *gzipCompressor) resetReader(msg []byte) error {
	if !g.readerInitialised {
		g.bytesReader = bytes.NewReader(msg)
		gzipReader, err := gzip.NewReader(g.bytesReader)
		if err != nil {
			return err
		}
		g.gzipReader = gzipReader
	} else {
		g.bytesReader.Reset(msg)
		if err := g.gzipReader.Reset(g.bytesReader); err != nil && err != io.EOF {
			return err
		}
	}
	return nil
}

// NewCompressor returns a new compressor instance
func NewCompressor() Compressor {
	return &gzipCompressor{}
}
