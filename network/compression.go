package network

import (
	"bytes"
	"compress/gzip"
	"io/ioutil"
)

type Compressor interface {
	Compress([]byte) ([]byte, error)
}

// gzipCompressor GZip Compressor implementation
type gzipCompressor struct {
	writer *gzip.Writer
	buffer *bytes.Buffer
}

// Compress compresses given bytes and returns them
// In case of any errors, the original bytes and error are returned.
func (w *gzipCompressor) Compress(msg []byte) ([]byte, error) {
	if w.buffer == nil {
		w.init()
	} else {
		w.reset()
	}

	_, err := w.writer.Write(msg)
	if err != nil {
		return msg, err
	}

	err = w.writer.Flush()
	if err != nil {
		return msg, err
	}

	err = w.writer.Close()
	if err != nil {
		return msg, err
	}

	return w.buffer.Bytes(), nil
}

// Reset resets the state for the next use
func (w *gzipCompressor) reset() {
	w.buffer.Reset()
	w.writer.Reset(w.buffer)
}

func (w *gzipCompressor) init() {
	var buffer bytes.Buffer
	w.buffer = &buffer
	w.writer = gzip.NewWriter(w.buffer)
}

// NewCompressor returns a new compressor instance
func NewCompressor() Compressor {
	return &gzipCompressor{}
}

type Decompressor interface {
	Decompress([]byte) ([]byte, error)
	IsCompressed([]byte) bool
}

type gzipDecompressor struct {
	buffer *bytes.Reader
	reader *gzip.Reader
}

func (g *gzipDecompressor) Decompress(msg []byte) ([]byte, error) {
	if g.buffer == nil {
		err := g.init(msg)
		if err != nil {
			return msg, err
		}
	} else {
		g.buffer.Reset(msg)
		err := g.reader.Reset(g.buffer)
		if err != nil {
			return msg, err
		}
	}

	data, err := ioutil.ReadAll(g.reader)
	if err != nil {
		return msg, err
	}

	err = g.reader.Close()
	if err != nil {
		return msg, err
	}

	return data, nil
}

func (g *gzipDecompressor) IsCompressed(msg []byte) bool {
	return len(msg) > 2 && msg[0] == 31 && msg[1] == 139
}

func (g *gzipDecompressor) init(msg []byte) error {
	g.buffer = bytes.NewReader(msg)
	reader, err := gzip.NewReader(g.buffer)
	if err != nil {
		return err
	}
	g.reader = reader
	return nil
}

func NewDecompressor() Decompressor {
	return &gzipDecompressor{}
}
