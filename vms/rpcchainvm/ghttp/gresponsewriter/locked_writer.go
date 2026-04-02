// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gresponsewriter

import (
	"bufio"
	"net"
	"net/http"
	"sync"
)

var (
	_ http.ResponseWriter = (*lockedWriter)(nil)
	_ http.Flusher        = (*lockedWriter)(nil)
	_ http.Hijacker       = (*lockedWriter)(nil)
)

type lockedWriter struct {
	lock          sync.Mutex
	writer        http.ResponseWriter
	headerWritten bool
}

func NewLockedWriter(w http.ResponseWriter) http.ResponseWriter {
	return &lockedWriter{writer: w}
}

func (lw *lockedWriter) Header() http.Header {
	lw.lock.Lock()
	defer lw.lock.Unlock()

	return lw.writer.Header()
}

func (lw *lockedWriter) Write(b []byte) (int, error) {
	lw.lock.Lock()
	defer lw.lock.Unlock()

	lw.headerWritten = true
	return lw.writer.Write(b)
}

func (lw *lockedWriter) WriteHeader(statusCode int) {
	lw.lock.Lock()
	defer lw.lock.Unlock()

	// Skip writing the header if it has already been written once.
	if lw.headerWritten {
		return
	}
	lw.headerWritten = true
	lw.writer.WriteHeader(statusCode)
}

func (lw *lockedWriter) Flush() {
	lw.lock.Lock()
	defer lw.lock.Unlock()

	flusher, ok := lw.writer.(http.Flusher)
	if ok {
		flusher.Flush()
	}
}

func (lw *lockedWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	lw.lock.Lock()
	defer lw.lock.Unlock()

	hijacker, ok := lw.writer.(http.Hijacker)
	if !ok {
		return nil, nil, errUnsupportedHijacking
	}
	return hijacker.Hijack()
}
