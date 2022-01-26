// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/perms"
)

var (
	fileSuffix = "utils/logging/log.go"
	filePrefix string
	timeFormat = "01-02|15:04:05.000"
)

func init() {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("failed to get file")
	}
	if len(file) < len(fileSuffix) {
		panic("unexpected file name returned")
	}
	filePrefix = file[:len(file)-len(fileSuffix)]
}

// Log implements the Logger interface
type Log struct {
	config Config

	messages []string
	size     int

	wg                               sync.WaitGroup
	flushLock, writeLock, configLock sync.Mutex
	needsFlush                       *sync.Cond

	closed bool

	writer RotatingWriter
}

// New returns a new logger set up according to [config]
func newLog(config Config) (*Log, error) {
	if err := os.MkdirAll(config.Directory, perms.ReadWriteExecute); err != nil {
		return nil, err
	}
	l := &Log{
		config: config,
		writer: &fileWriter{},
	}
	l.needsFlush = sync.NewCond(&l.flushLock)

	l.wg.Add(1)

	go l.RecoverAndPanic(l.run)

	return l, nil
}

func (l *Log) run() {
	defer l.wg.Done()

	l.writeLock.Lock()
	defer l.writeLock.Unlock()

	currentSize, err := l.writer.Initialize(l.config)
	if err != nil {
		panic(err)
	}

	closed := false
	nextRotation := time.Now().Add(l.config.RotationInterval)

	for !closed {
		l.writeLock.Unlock()
		l.flushLock.Lock()
		for l.size < l.config.FlushSize && !l.closed {
			l.needsFlush.Wait()
		}
		closed = l.closed
		prevMessages := l.messages
		l.messages = nil
		l.size = 0
		l.flushLock.Unlock()
		l.writeLock.Lock()

		for _, msg := range prevMessages {
			n, _ := l.writer.WriteString(msg)
			currentSize += n
		}

		if !l.config.DisableFlushOnWrite {
			// attempt to flush after the write
			_ = l.writer.Flush()
		}

		if now := time.Now(); nextRotation.Before(now) || currentSize > l.config.FileSize {
			nextRotation = now.Add(l.config.RotationInterval)
			currentSize = 0
			// attempt to flush before closing
			_ = l.writer.Flush()
			// attempt to close the file
			_ = l.writer.Close()

			if err := l.writer.Rotate(); err != nil {
				panic(err)
			}
		}
	}
	// attempt to flush when exiting
	_ = l.writer.Flush()
	// attempt to close the file when exiting
	_ = l.writer.Close()
}

func (l *Log) Write(p []byte) (int, error) {
	if l == nil {
		return 0, nil
	}

	l.configLock.Lock()
	defer l.configLock.Unlock()

	if !l.config.DisableLogging {
		l.flushLock.Lock()
		l.messages = append(l.messages, string(p))
		l.size += len(p)
		l.needsFlush.Signal()
		l.flushLock.Unlock()
	}

	return len(p), nil
}

// Stop implements the Logger interface
func (l *Log) Stop() {
	l.flushLock.Lock()
	l.closed = true
	l.needsFlush.Signal()
	l.flushLock.Unlock()

	l.wg.Wait()
}

// Should only be called from [Level] functions.
func (l *Log) log(level Level, format string, args ...interface{}) {
	if l == nil {
		return
	}

	l.configLock.Lock()
	defer l.configLock.Unlock()

	shouldLog := !l.config.DisableLogging && level <= l.config.LogLevel
	shouldDisplay := (!l.config.DisableDisplaying && level <= l.config.DisplayLevel) || level == Fatal

	if !shouldLog && !shouldDisplay {
		return
	}

	args = SanitizeArgs(args)

	output := l.format(level, format, args...)

	if shouldLog {
		l.flushLock.Lock()
		l.messages = append(l.messages, output)
		l.size += len(output)
		l.needsFlush.Signal()
		l.flushLock.Unlock()
	}

	if shouldDisplay {
		switch {
		case l.config.DisableContextualDisplaying:
			fmt.Println(fmt.Sprintf(format, args...))
		case l.config.DisplayHighlight == Plain:
			fmt.Print(output)
		default:
			fmt.Print(level.Color().Wrap(output))
		}
	}
}

func (l *Log) format(level Level, format string, args ...interface{}) string {
	loc := "?"
	if _, file, no, ok := runtime.Caller(3); ok {
		localFile := strings.TrimPrefix(file, filePrefix)
		loc = fmt.Sprintf("%s#%d", localFile, no)
	}
	text := fmt.Sprintf("%s: %s", loc, fmt.Sprintf(format, args...))

	prefix := ""
	if l.config.MsgPrefix != "" {
		prefix = fmt.Sprintf(" <%s>", l.config.MsgPrefix)
	}

	return fmt.Sprintf("%s[%s]%s %s\n",
		level.AlignedString(),
		time.Now().Format(timeFormat),
		prefix,
		text)
}

// Fatal implements the Logger interface
func (l *Log) Fatal(format string, args ...interface{}) { l.log(Fatal, format, args...) }

// Error implements the Logger interface
func (l *Log) Error(format string, args ...interface{}) { l.log(Error, format, args...) }

// Warn implements the Logger interface
func (l *Log) Warn(format string, args ...interface{}) { l.log(Warn, format, args...) }

// Info implements the Logger interface
func (l *Log) Info(format string, args ...interface{}) { l.log(Info, format, args...) }

// Trace implements the Logger interface
func (l *Log) Trace(format string, args ...interface{}) { l.log(Trace, format, args...) }

// Debug implements the Logger interface
func (l *Log) Debug(format string, args ...interface{}) { l.log(Debug, format, args...) }

// Verbo implements the Logger interface
func (l *Log) Verbo(format string, args ...interface{}) { l.log(Verbo, format, args...) }

// AssertNoError implements the Logger interface
func (l *Log) AssertNoError(err error) {
	if err != nil {
		l.log(Fatal, "%s", err)
	}
	if l.config.Assertions && err != nil {
		l.Stop()
		panic(err)
	}
}

// AssertTrue implements the Logger interface
func (l *Log) AssertTrue(b bool, format string, args ...interface{}) {
	if !b {
		l.log(Fatal, format, args...)
	}
	if l.config.Assertions && !b {
		l.Stop()
		panic(fmt.Sprintf(format, args...))
	}
}

// AssertDeferredTrue implements the Logger interface
func (l *Log) AssertDeferredTrue(f func() bool, format string, args ...interface{}) {
	// Note, the logger will only be notified here if assertions are enabled
	if l.config.Assertions && !f() {
		err := fmt.Sprintf(format, args...)
		l.log(Fatal, err)
		l.Stop()
		panic(err)
	}
}

// AssertDeferredNoError implements the Logger interface
func (l *Log) AssertDeferredNoError(f func() error) {
	if l.config.Assertions {
		err := f()
		if err != nil {
			l.log(Fatal, "%s", err)
		}
		if l.config.Assertions && err != nil {
			l.Stop()
			panic(err)
		}
	}
}

// StopOnPanic implements the Logger interface
func (l *Log) StopOnPanic() {
	if r := recover(); r != nil {
		l.Fatal("Panicking due to:\n%s\nFrom:\n%s", r, Stacktrace{})
		l.Stop()
		panic(r)
	}
}

// RecoverAndPanic implements the Logger interface
func (l *Log) RecoverAndPanic(f func()) { defer l.StopOnPanic(); f() }

func (l *Log) stopAndExit(exit func()) {
	if r := recover(); r != nil {
		l.Fatal("Panicking due to:\n%s\nFrom:\n%s", r, Stacktrace{})
		l.Stop()
		exit()
	}
}

// RecoverAndExit implements the Logger interface
func (l *Log) RecoverAndExit(f, exit func()) { defer l.stopAndExit(exit); f() }

func (l *Log) SetLogLevel(lvl Level) {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	l.config.LogLevel = lvl
}

// GetLogLevel ...
func (l *Log) GetLogLevel() Level {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	return l.config.LogLevel
}

// GetDisplayLevel implements the Logger interface
func (l *Log) GetDisplayLevel() Level {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	return l.config.DisplayLevel
}

// SetDisplayLevel implements the Logger interface
func (l *Log) SetDisplayLevel(lvl Level) {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	l.config.DisplayLevel = lvl
}

// SetPrefix implements the Logger interface
func (l *Log) SetPrefix(prefix string) {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	l.config.MsgPrefix = prefix
}

// SetLoggingEnabled implements the Logger interface
func (l *Log) SetLoggingEnabled(enabled bool) {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	l.config.DisableLogging = !enabled
}

// SetDisplayingEnabled implements the Logger interface
func (l *Log) SetDisplayingEnabled(enabled bool) {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	l.config.DisableDisplaying = !enabled
}

// SetContextualDisplayingEnabled implements the Logger interface
func (l *Log) SetContextualDisplayingEnabled(enabled bool) {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	l.config.DisableContextualDisplaying = !enabled
}

// fileWriter implements the RotatingWriter interface
type fileWriter struct {
	writer *bufio.Writer
	file   *os.File

	config Config
}

// Flush implements the RotatingWriter interface
func (fw *fileWriter) Flush() error {
	return fw.writer.Flush()
}

// Write implements the RotatingWriter interface
func (fw *fileWriter) Write(b []byte) (int, error) {
	return fw.writer.Write(b)
}

// WriteString implements the RotatingWriter interface
func (fw *fileWriter) WriteString(s string) (int, error) {
	return fw.writer.WriteString(s)
}

// Close implements the RotatingWriter interface
func (fw *fileWriter) Close() error {
	return fw.file.Close()
}

// Rotate implements the RotatingWriter interface
func (fw *fileWriter) Rotate() error {
	for i := fw.config.RotationSize - 1; i > 0; i-- {
		sourceFilename := filepath.Join(fw.config.Directory, fmt.Sprintf("%s.log.%d", fw.config.LoggerName, i))
		destFilename := filepath.Join(fw.config.Directory, fmt.Sprintf("%s.log.%d", fw.config.LoggerName, i+1))
		if _, err := os.Stat(sourceFilename); !errors.Is(err, os.ErrNotExist) {
			if err := os.Rename(sourceFilename, destFilename); err != nil {
				return err
			}
		}
	}
	sourceFilename := filepath.Join(fw.config.Directory, fmt.Sprintf("%s.log", fw.config.LoggerName))
	destFilename := filepath.Join(fw.config.Directory, fmt.Sprintf("%s.log.1", fw.config.LoggerName))
	if err := os.Rename(sourceFilename, destFilename); err != nil {
		return err
	}
	writer, file, err := fw.create()
	if err != nil {
		return err
	}
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

// Initialize implements the RotatingWriter interface
func (fw *fileWriter) Initialize(config Config) (int, error) {
	fw.config = config
	writer, file, err := fw.create()
	if err != nil {
		return 0, err
	}
	fw.writer = writer
	fw.file = file
	fileinfo, err := file.Stat()
	if err != nil {
		return 0, err
	}
	fileSize := fileinfo.Size()
	return int(fileSize), nil
}
