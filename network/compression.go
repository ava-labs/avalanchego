// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"bytes"
	"compress/gzip"
	"io"
)

const minCompressSize = 128

// Compressor compresss and decompresses messages.
// Decompress is the inverse of Compress.
// Decompress(Compress(msg)) == msg
type Compressor interface {
	Compress([]byte) ([]byte, error)
	Decompress([]byte) ([]byte, error)
}

// gzipCompressor implements Compressor
type gzipCompressor struct {
	// Compress messages with length >= [minCompressSize]
	minCompressSize int

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
		return nil, err
	}
	if err := g.gzipWriter.Close(); err != nil {
		return nil, err
	}
	return g.writeBuffer.Bytes(), nil
}

// Decompress decompresses [msg].
func (g *gzipCompressor) Decompress(msg []byte) ([]byte, error) {
	if err := g.resetReader(msg); err != nil {
		return nil, err
	}

	data, err := io.ReadAll(g.gzipReader)
	if err != nil {
		return nil, err
	}

	if err = g.gzipReader.Close(); err != nil {
		return nil, err
	}

	return data, nil
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

// NewCompressor returns a new Compressor that compresses
// messages with length >=
func NewCompressor(minCompressSize int) Compressor {
	return &gzipCompressor{
		minCompressSize: minCompressSize,
	}
}
