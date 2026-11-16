// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wal

import (
	"fmt"
	"io"
	"os"
)

const (
	WalFlags       = os.O_APPEND | os.O_CREATE | os.O_RDWR
	WalPermissions = 0600
)

type WriteAheadLog struct {
	file     *os.File
	fileName string
}

// New opens a write ahead log file, creating one if necessary.
// Call Close() on the WriteAheadLog to ensure the file is closed after use.
func New(fileName string) *WriteAheadLog {
	return &WriteAheadLog{
		fileName: fileName,
	}
}

func (w *WriteAheadLog) maybeOpenFile() error {
	if w.file != nil {
		return nil
	}
	file, err := os.OpenFile(w.fileName, WalFlags, WalPermissions)
	if err != nil {
		return err
	}
	w.file = file
	return nil
}

// Appends a record to the write ahead log
// Must flush the OS cache on every append to ensure consistency
func (w *WriteAheadLog) Append(b []byte) error {
	if err := w.maybeOpenFile(); err != nil {
		return err
	}
	// writeRecord will append
	if err := writeRecord(w.file, b); err != nil {
		return err
	}

	// ensure file gets written to persistent storage
	return w.file.Sync()
}

func (w *WriteAheadLog) Delete() error {
	if w.file == nil {
		return os.Remove(w.fileName)
	}

	if err := w.file.Close(); err != nil {
		return err
	}

	return os.Remove(w.file.Name())
}

func (w *WriteAheadLog) ReadAll() ([][]byte, error) {
	if err := w.maybeOpenFile(); err != nil {
		return nil, err
	}
	_, err := w.file.Seek(0, io.SeekStart)
	if err != nil {
		return nil, fmt.Errorf("error seeking to start %w", err)
	}

	fileInfo, err := w.file.Stat()
	if err != nil {
		return nil, fmt.Errorf("error getting file info %w", err)
	}
	bytesToRead := fileInfo.Size()

	var payloads [][]byte
	for bytesToRead > 0 {
		payload, bytesRead, err := readRecord(w.file, bytesToRead)
		// record was corrupted in wal
		if err != nil {
			return payloads, w.truncateAt(fileInfo.Size() - bytesToRead)
		}

		bytesToRead -= bytesRead
		payloads = append(payloads, payload)
	}

	// should never happen
	if bytesToRead != 0 {
		return payloads, fmt.Errorf("read more bytes than expected")
	}

	return payloads, nil
}

func (w *WriteAheadLog) truncateAt(offset int64) error {
	// truncate call is atomic. Ref https://cgi.cse.unsw.edu.au/~cs3231/18s1/os161/man/syscall/ftruncate.html
	err := w.file.Truncate(offset)
	if err != nil {
		return err
	}

	return w.file.Sync()
}

func (w *WriteAheadLog) Close() error {
	if w.file == nil {
		return nil
	}
	defer func() {
		w.file = nil
	}()
	return w.file.Close()
}
