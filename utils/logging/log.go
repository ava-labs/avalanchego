// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Log ...
type Log struct {
	config Config

	messages []string
	size     int

	wg                               sync.WaitGroup
	flushLock, writeLock, configLock sync.Mutex
	needsFlush                       *sync.Cond
	w                                *bufio.Writer

	closed bool
}

// New ...
func New(config Config) (*Log, error) {
	if err := os.MkdirAll(config.Directory, os.ModePerm); err != nil {
		return nil, err
	}
	l := &Log{config: config}
	l.needsFlush = sync.NewCond(&l.flushLock)

	l.wg.Add(1)

	go l.RecoverAndPanic(l.run)

	return l, nil
}

func (l *Log) run() {
	defer l.wg.Done()

	l.writeLock.Lock()
	defer l.writeLock.Unlock()

	fileIndex := 0
	filename := path.Join(l.config.Directory, fmt.Sprintf("%d.log", fileIndex))
	f, err := os.Create(filename)
	if err != nil {
		panic(err)
	}
	l.w = bufio.NewWriter(f)

	closed := false
	nextRotation := time.Now().Add(l.config.RotationInterval)
	currentSize := 0
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
			n, _ := l.w.WriteString(msg)
			currentSize += n
		}

		if !l.config.DisableFlushOnWrite {
			l.w.Flush()
		}

		if now := time.Now(); nextRotation.Before(now) || currentSize > l.config.FileSize {
			nextRotation = now.Add(l.config.RotationInterval)
			currentSize = 0
			l.w.Flush()
			f.Close()

			fileIndex = (fileIndex + 1) % l.config.RotationSize
			filename := path.Join(l.config.Directory, fmt.Sprintf("%d.log", fileIndex))
			f, err = os.Create(filename)
			if err != nil {
				panic(err)
			}
			l.w = bufio.NewWriter(f)
		}
	}
	l.w.Flush()
	f.Close()
}

func (l *Log) Write(p []byte) (int, error) {
	l.writeLock.Lock()
	defer l.writeLock.Unlock()

	return l.w.Write(p)
}

// Stop ...
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

	output := l.format(level, format, args...)

	if shouldLog {
		l.flushLock.Lock()
		l.messages = append(l.messages, output)
		l.size += len(output)
		l.needsFlush.Signal()
		l.flushLock.Unlock()
	}

	if shouldDisplay {
		if l.config.DisableContextualDisplaying {
			fmt.Println(fmt.Sprintf(format, args...))
		} else {
			fmt.Print(level.Color().Wrap(output))
		}
	}
}

func (l *Log) format(level Level, format string, args ...interface{}) string {
	loc := "?"
	if _, file, no, ok := runtime.Caller(3); ok {
		loc = fmt.Sprintf("%s#%d", file, no)
	}
	if i := strings.Index(loc, "gecko/"); i != -1 {
		loc = loc[i+5:]
	}
	text := fmt.Sprintf("%s: %s", loc, fmt.Sprintf(format, args...))

	prefix := ""
	if l.config.MsgPrefix != "" {
		prefix = fmt.Sprintf(" <%s>", l.config.MsgPrefix)
	}

	return fmt.Sprintf("%s[%s]%s %s\n",
		level,
		time.Now().Format("01-02|15:04:05"),
		prefix,
		text)
}

// Fatal ...
func (l *Log) Fatal(format string, args ...interface{}) { l.log(Fatal, format, args...) }

// Error ...
func (l *Log) Error(format string, args ...interface{}) { l.log(Error, format, args...) }

// Warn ...
func (l *Log) Warn(format string, args ...interface{}) { l.log(Warn, format, args...) }

// Info ...
func (l *Log) Info(format string, args ...interface{}) { l.log(Info, format, args...) }

// Debug ...
func (l *Log) Debug(format string, args ...interface{}) { l.log(Debug, format, args...) }

// Verbo ...
func (l *Log) Verbo(format string, args ...interface{}) { l.log(Verbo, format, args...) }

// AssertNoError ...
func (l *Log) AssertNoError(err error) {
	if err != nil {
		l.log(Fatal, "%s", err)
	}
	if l.config.Assertions && err != nil {
		l.Stop()
		panic(err)
	}
}

// AssertTrue ...
func (l *Log) AssertTrue(b bool, format string, args ...interface{}) {
	if !b {
		l.log(Fatal, format, args...)
	}
	if l.config.Assertions && !b {
		l.Stop()
		panic(fmt.Sprintf(format, args...))
	}
}

// AssertDeferredTrue ...
func (l *Log) AssertDeferredTrue(f func() bool, format string, args ...interface{}) {
	// Note, the logger will only be notified here if assertions are enabled
	if l.config.Assertions && !f() {
		err := fmt.Sprintf(format, args...)
		l.log(Fatal, err)
		l.Stop()
		panic(err)
	}
}

// AssertDeferredNoError ...
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

// StopOnPanic ...
func (l *Log) StopOnPanic() {
	if r := recover(); r != nil {
		l.Fatal("Panicing due to:\n%s\nFrom:\n%s", r, Stacktrace{})
		l.Stop()
		panic(r)
	}
}

// RecoverAndPanic ...
func (l *Log) RecoverAndPanic(f func()) { defer l.StopOnPanic(); f() }

// SetLogLevel ...
func (l *Log) SetLogLevel(lvl Level) {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	l.config.LogLevel = lvl
}

// SetDisplayLevel ...
func (l *Log) SetDisplayLevel(lvl Level) {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	l.config.DisplayLevel = lvl
}

// SetPrefix ...
func (l *Log) SetPrefix(prefix string) {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	l.config.MsgPrefix = prefix
}

// SetLoggingEnabled ...
func (l *Log) SetLoggingEnabled(enabled bool) {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	l.config.DisableLogging = !enabled
}

// SetDisplayingEnabled ...
func (l *Log) SetDisplayingEnabled(enabled bool) {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	l.config.DisableDisplaying = !enabled
}

// SetContextualDisplayingEnabled ...
func (l *Log) SetContextualDisplayingEnabled(enabled bool) {
	l.configLock.Lock()
	defer l.configLock.Unlock()

	l.config.DisableContextualDisplaying = !enabled
}
