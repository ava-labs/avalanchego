package network

import (
	"bytes"
	"compress/gzip"
	"sync"
)

type Compressor interface {
	Compress([]byte) ([]byte, error)
	Reset()
}

// gzipCompressor GZip Compressor implementation
type gzipCompressor struct {
	writer *gzip.Writer
	buffer *bytes.Buffer
}

// Compress compresses given bytes and returns them
// In case of any errors, the original bytes and error are returned.
func (w gzipCompressor) Compress(msg []byte) ([]byte, error) {
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
func (w gzipCompressor) Reset() {
	w.buffer.Reset()
	w.writer.Reset(w.buffer)
}

// NewCompressor returns a new compressor instance
func NewCompressor() Compressor {
	var buffer bytes.Buffer
	gWriter := gzip.NewWriter(&buffer)

	return gzipCompressor{
		writer: gWriter,
		buffer: &buffer,
	}
}

func NewCompressorPool() sync.Pool {
	return sync.Pool{
		New: func() interface{} {
			return NewCompressor()
		},
	}
}
