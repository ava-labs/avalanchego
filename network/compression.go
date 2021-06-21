// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"bytes"
	"compress/gzip"
	"io"
)

type Compressor interface {
	Compress([]byte) ([]byte, error)
	Decompress([]byte) ([]byte, error)
	IsCompressed([]byte) bool
}

// gzipCompressor implements Compressor
type gzipCompressor struct {
	initialized bool
	bytesReader *bytes.Reader
	gzipReader  *gzip.Reader
	gzipWriter  *gzip.Writer
	writeBuffer *bytes.Buffer
}

// Compress [msg] and returns the compressed bytes.
func (g *gzipCompressor) Compress(msg []byte) ([]byte, error) {
	if err := g.reset(); err != nil {
		return nil, err
	}
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
	if err := g.reset(); err != nil {
		return nil, err
	}
	if g.gzipReader == nil {
		g.bytesReader = bytes.NewReader(msg)
		gzipReader, err := gzip.NewReader(g.bytesReader)
		if err != nil {
			return nil, err
		}
		g.gzipReader = gzipReader
	} else {
		g.bytesReader.Reset(msg)
		if err := g.gzipReader.Reset(g.bytesReader); err != nil && err != io.EOF {
			return nil, err
		}
	}
	return io.ReadAll(g.gzipReader)
}

// IsCompressed returns whether given bytes are compressed data or not
func (g *gzipCompressor) IsCompressed(msg []byte) bool {
	return len(msg) > 2 && msg[0] == 31 && msg[1] == 139
}

func (g *gzipCompressor) reset() error {
	if !g.initialized {
		var buf bytes.Buffer
		g.writeBuffer = &buf
		g.gzipWriter = gzip.NewWriter(g.writeBuffer)
		g.initialized = true
	}
	g.writeBuffer.Reset()
	g.gzipWriter.Reset(g.writeBuffer)
	return nil
}

// NewCompressor returns a new compressor instance
func NewCompressor() Compressor {
	return &gzipCompressor{}
}
