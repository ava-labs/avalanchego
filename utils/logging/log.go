// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

var (
	fileSuffix = "utils/logging/log.go"
	filePrefix string
	timeFormat = "01-02|15:04:05.000"

	_ Logger = &Log{}
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
func newLog(config Config) Logger {
	l := &Log{
		config: config,
		writer: &fileWriter{},
	}
	l.needsFlush = sync.NewCond(&l.flushLock)

	l.wg.Add(1)

	go l.RecoverAndPanic(l.run)

	return l
}

func (l *Log) run() {
	defer l.wg.Done()

	l.writeLock.Lock()
	defer l.writeLock.Unlock()

	l.writer.Initialize(l.config)

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
			_, _ = l.writer.WriteString(msg)
		}

		if !l.config.DisableFlushOnWrite {
			// attempt to flush after the write
			_ = l.writer.Flush()
		}

		if now := time.Now(); nextRotation.Before(now) || l.writer.GetCurrentSize() > l.config.FileSize {
			nextRotation = now.Add(l.config.RotationInterval)
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

	l.flushLock.Lock()
	l.messages = append(l.messages, string(p))
	l.size += len(p)
	l.needsFlush.Signal()
	l.flushLock.Unlock()

	if !l.config.DisableWriterDisplaying {
		os.Stdout.Write(p)
	}

	return len(p), nil
}

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

	shouldLog := level <= l.config.LogLevel
	shouldDisplay := level <= l.config.DisplayLevel

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

func (l *Log) Fatal(format string, args ...interface{}) { l.log(Fatal, format, args...) }

func (l *Log) Error(format string, args ...interface{}) { l.log(Error, format, args...) }

func (l *Log) Warn(format string, args ...interface{}) { l.log(Warn, format, args...) }

func (l *Log) Info(format string, args ...interface{}) { l.log(Info, format, args...) }

func (l *Log) Trace(format string, args ...interface{}) { l.log(Trace, format, args...) }

func (l *Log) Debug(format string, args ...interface{}) { l.log(Debug, format, args...) }

func (l *Log) Verbo(format string, args ...interface{}) { l.log(Verbo, format, args...) }

func (l *Log) AssertNoError(err error) {
	if err != nil {
		l.log(Fatal, "%s", err)
	}
	if l.config.Assertions && err != nil {
		l.Stop()
		panic(err)
	}
}

func (l *Log) AssertTrue(b bool, format string, args ...interface{}) {
	if !b {
		l.log(Fatal, format, args...)
	}
	if l.config.Assertions && !b {
		l.Stop()
		panic(fmt.Sprintf(format, args...))
	}
}

func (l *Log) AssertDeferredTrue(f func() bool, format string, args ...interface{}) {
	// Note, the logger will only be notified here if assertions are enabled
	if l.config.Assertions && !f() {
		err := fmt.Sprintf(format, args...)
		l.log(Fatal, err)
		l.Stop()
		panic(err)
	}
}

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

func (l *Log) StopOnPanic() {
	if r := recover(); r != nil {
		l.Fatal("Panicking due to:\n%s\nFrom:\n%s", r, Stacktrace{})
		l.Stop()
		panic(r)
	}
}

func (l *Log) RecoverAndPanic(f func()) { defer l.StopOnPanic(); f() }

func (l *Log) stopAndExit(exit func()) {
	if r := recover(); r != nil {
		l.Fatal("Panicking due to:\n%s\nFrom:\n%s", r, Stacktrace{})
		l.Stop()
		exit()
	}
}

func (l *Log) RecoverAndExit(f, exit func()) { defer l.stopAndExit(exit); f() }

func (l *Log) SetLogLevel(lvl Level) {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	l.config.LogLevel = lvl
}

func (l *Log) GetLogLevel() Level {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	return l.config.LogLevel
}

func (l *Log) GetDisplayLevel() Level {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	return l.config.DisplayLevel
}

func (l *Log) SetDisplayLevel(lvl Level) {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	l.config.DisplayLevel = lvl
}

func (l *Log) SetPrefix(prefix string) {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	l.config.MsgPrefix = prefix
}

func (l *Log) SetContextualDisplayingEnabled(enabled bool) {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	l.config.DisableContextualDisplaying = !enabled
}
