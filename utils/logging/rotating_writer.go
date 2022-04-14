// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ava-labs/avalanchego/utils/filesystem"
	"github.com/ava-labs/avalanchego/utils/perms"
)

var _ RotatingWriter = &fileWriter{}

// RotatingWriter allows for rotating a stream writer
type RotatingWriter interface {
	// Creates the log file if it doesn't exist or resume writing to it if it does
	Initialize(Config)
	// Flushes the writer
	Flush() error
	// Writes [b] to the log file
	Write(b []byte) (int, error)
	// Writes [s] to the log file
	WriteString(s string) (int, error)
	// Closes the log file
	Close() error
	// GetCurrentSize returns the size of the current file being written to
	GetCurrentSize() int
	// Rotates the log files. Always keeps the current log in the same file.
	// Rotated log files are stored as by appending an integer to the log file name,
	// from 1 to the RotationSize defined in the configuration. 1 being the most
	// recently rotated log file.
	Rotate() error
}

// fileWriter is a non-thread safe implementation of a RotatingWriter.
type fileWriter struct {
	writer      *bufio.Writer
	file        *os.File
	fileSize    int
	initialized bool

	config Config
}

func (fw *fileWriter) Initialize(config Config) {
	fw.config = config
}

func (fw *fileWriter) Flush() error {
	if !fw.initialized {
		return nil
	}
	return fw.writer.Flush()
}

func (fw *fileWriter) Write(b []byte) (int, error) {
	if err := fw.init(); err != nil {
		return 0, err
	}
	n, err := fw.writer.Write(b)
	fw.fileSize += n
	return n, err
}

func (fw *fileWriter) WriteString(s string) (int, error) {
	if err := fw.init(); err != nil {
		return 0, err
	}
	n, err := fw.writer.WriteString(s)
	fw.fileSize += n
	return n, err
}

func (fw *fileWriter) Close() error {
	if !fw.initialized {
		return nil
	}
	return fw.file.Close()
}

func (fw *fileWriter) GetCurrentSize() int {
	return fw.fileSize
}

func (fw *fileWriter) Rotate() error {
	for i := fw.config.RotationSize - 1; i > 0; i-- {
		sourceFilename := filepath.Join(fw.config.Directory, fmt.Sprintf("%s.log.%d", fw.config.LoggerName, i))
		destFilename := filepath.Join(fw.config.Directory, fmt.Sprintf("%s.log.%d", fw.config.LoggerName, i+1))
		if _, err := filesystem.RenameIfExists(sourceFilename, destFilename); err != nil {
			return err
		}
	}
	sourceFilename := filepath.Join(fw.config.Directory, fmt.Sprintf("%s.log", fw.config.LoggerName))
	destFilename := filepath.Join(fw.config.Directory, fmt.Sprintf("%s.log.1", fw.config.LoggerName))
	if _, err := filesystem.RenameIfExists(sourceFilename, destFilename); err != nil {
		return err
	}
	writer, file, err := fw.create()
	if err != nil {
		return err
	}
	fw.fileSize = 0
	fw.file = file
	fw.writer = writer
	return nil
}

// Creates a file if it does not exist or opens it in append mode if it does
func (fw *fileWriter) create() (*bufio.Writer, *os.File, error) {
	filename := filepath.Join(fw.config.Directory, fmt.Sprintf("%s.log", fw.config.LoggerName))
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, perms.ReadWrite)
	if err != nil {
		return nil, nil, err
	}
	writer := bufio.NewWriter(file)
	return writer, file, nil
}

func (fw *fileWriter) init() error {
	if fw.initialized {
		return nil
	}
	if err := os.MkdirAll(fw.config.Directory, perms.ReadWriteExecute); err != nil {
		return err
	}

	writer, file, err := fw.create()
	if err != nil {
		return err
	}
	fw.writer = writer
	fw.file = file
	fileinfo, err := file.Stat()
	if err != nil {
		return err
	}
	fw.fileSize = int(fileinfo.Size())
	fw.initialized = true
	return nil
}
