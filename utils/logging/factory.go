// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"fmt"
	"os"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/exp/maps"
	"gopkg.in/natefinch/lumberjack.v2"
)

var _ Factory = (*factory)(nil)

// Factory creates new instances of different types of Logger
type Factory interface {
	// Make creates a new logger with name [name]
	Make(name string) (Logger, error)

	// MakeChain creates a new logger to log the events of chain [chainID]
	MakeChain(chainID string) (Logger, error)

	// SetLogLevel sets log levels for all loggers in factory with given logger name, level pairs.
	SetLogLevel(name string, level Level) error

	// SetDisplayLevel sets log display levels for all loggers in factory with given logger name, level pairs.
	SetDisplayLevel(name string, level Level) error

	// GetLogLevel returns all log levels in factory as name, level pairs
	GetLogLevel(name string) (Level, error)

	// GetDisplayLevel returns all log display levels in factory as name, level pairs
	GetDisplayLevel(name string) (Level, error)

	// GetLoggerNames returns the names of all logs created by this factory
	GetLoggerNames() []string

	// Close stops and clears all of a Factory's instantiated loggers
	Close()

	// Amplify amplifies the logging level of all loggers to DEBUG.
	Amplify()

	// Revert reverts the logging level of all loggers to what it was before Amplify() was called.
	Revert()
}

type logWrapper struct {
	logger                  *log
	displayLevel            zap.AtomicLevel
	logLevel                zap.AtomicLevel
	longTermLogDisplayLevel uint32 // zapcore.Level to be accessed atomically
	longTermLogFileLevel    uint32 // zapcore.Level to be accessed atomically
}

type factory struct {
	config Config
	lock   sync.RWMutex

	// For each logger created by this factory:
	// Logger name --> the logger.
	loggers map[string]*logWrapper

	// Used to restrict logging amplification to a limited duration
	amplificationTracker amplificationDurationTracker
	// frozen is used to prevent amplification from activating once it has exhausted its maximal duration.
	// It is set to true once its maximal duration is exhausted, and is set to false once Revert() is called.
	frozen atomic.Bool
}

// NewFactory returns a new instance of a Factory producing loggers configured with
// the values set in the [config] parameter
func NewFactory(config Config) *factory {
	return &factory{
		config:  config,
		loggers: make(map[string]*logWrapper),
		amplificationTracker: amplificationDurationTracker{
			now:         time.Now,
			maxDuration: config.LoggingAutoAmplificationMaxDuration,
		},
	}
}

// Assumes [f.lock] is held
func (f *factory) makeLogger(config Config) (Logger, error) {
	if _, ok := f.loggers[config.LoggerName]; ok {
		return nil, fmt.Errorf("logger with name %q already exists", config.LoggerName)
	}
	consoleEnc := config.LogFormat.ConsoleEncoder()
	fileEnc := config.LogFormat.FileEncoder()

	consoleCore := NewWrappedCore(config.DisplayLevel, os.Stdout, consoleEnc)
	consoleCore.WriterDisabled = config.DisableWriterDisplaying

	rw := &lumberjack.Logger{
		Filename:   path.Join(config.Directory, config.LoggerName+".log"),
		MaxSize:    config.MaxSize,  // megabytes
		MaxAge:     config.MaxAge,   // days
		MaxBackups: config.MaxFiles, // files
		Compress:   config.Compress,
	}
	fileCore := NewWrappedCore(config.LogLevel, rw, fileEnc)
	prefix := config.LogFormat.WrapPrefix(config.MsgPrefix)

	l := NewLogger(prefix, consoleCore, fileCore)
	f.loggers[config.LoggerName] = &logWrapper{
		logger:                  l,
		displayLevel:            consoleCore.AtomicLevel,
		logLevel:                fileCore.AtomicLevel,
		longTermLogDisplayLevel: uint32(consoleCore.AtomicLevel.Level()),
		longTermLogFileLevel:    uint32(fileCore.AtomicLevel.Level()),
	}
	return l, nil
}

func (f *factory) Make(name string) (Logger, error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	config := f.config
	config.LoggerName = name
	return f.makeLogger(config)
}

func (f *factory) MakeChain(chainID string) (Logger, error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	config := f.config
	config.MsgPrefix = chainID + " Chain"
	config.LoggerName = chainID
	return f.makeLogger(config)
}

func (f *factory) SetLogLevel(name string, level Level) error {
	f.lock.RLock()
	defer f.lock.RUnlock()

	logger, ok := f.loggers[name]
	if !ok {
		return fmt.Errorf("logger with name %q not found", name)
	}
	logger.logLevel.SetLevel(zapcore.Level(level))
	atomic.StoreUint32(&logger.longTermLogFileLevel, uint32(level))
	return nil
}

func (f *factory) SetDisplayLevel(name string, level Level) error {
	f.lock.RLock()
	defer f.lock.RUnlock()

	logger, ok := f.loggers[name]
	if !ok {
		return fmt.Errorf("logger with name %q not found", name)
	}
	logger.displayLevel.SetLevel(zapcore.Level(level))
	atomic.StoreUint32(&logger.longTermLogDisplayLevel, uint32(level))

	return nil
}

func (f *factory) GetLogLevel(name string) (Level, error) {
	f.lock.RLock()
	defer f.lock.RUnlock()

	logger, ok := f.loggers[name]
	if !ok {
		return -1, fmt.Errorf("logger with name %q not found", name)
	}
	return Level(logger.logLevel.Level()), nil
}

func (f *factory) GetDisplayLevel(name string) (Level, error) {
	f.lock.RLock()
	defer f.lock.RUnlock()

	logger, ok := f.loggers[name]
	if !ok {
		return -1, fmt.Errorf("logger with name %q not found", name)
	}
	return Level(logger.displayLevel.Level()), nil
}

func (f *factory) GetLoggerNames() []string {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return maps.Keys(f.loggers)
}

func (f *factory) Close() {
	f.lock.Lock()
	defer f.lock.Unlock()

	for _, lw := range f.loggers {
		lw.logger.Stop()
	}
	f.loggers = nil
}

func (f *factory) setLoggingCoreLogLevel(logFunc func(logDisplayLevel zap.AtomicLevel, logFileLevel zap.AtomicLevel, originalDisplayLevel zapcore.Level, originalLogFileLevel zapcore.Level)) {
	f.lock.RLock()
	defer f.lock.RUnlock()

	for _, logger := range f.loggers {
		// This is a precaution to avoid crashing the node in case this code is executed in a different way we expect it to.
		if len(logger.logger.wrappedCores) == 2 {
			displayLevel := zapcore.Level(atomic.LoadUint32(&logger.longTermLogDisplayLevel))
			logFileLevel := zapcore.Level(atomic.LoadUint32(&logger.longTermLogFileLevel))
			logFunc(logger.logger.wrappedCores[0].AtomicLevel, logger.logger.wrappedCores[1].AtomicLevel, displayLevel, logFileLevel)
		}
	}
}

func (f *factory) Amplify() {
	if f.amplificationDisabled() || f.frozen.Load() {
		return
	}

	if f.amplificationTracker.isAmplificationDurationExhausted() {
		f.revertLoggers()
		f.frozen.Store(true)
		return
	}

	f.setLoggingCoreLogLevel(func(logDisplayLevel zap.AtomicLevel, logFileLevel zap.AtomicLevel, _ zapcore.Level, _ zapcore.Level) {
		if logDisplayLevel.Level() > zapcore.Level(Debug) {
			logDisplayLevel.SetLevel(zapcore.Level(Debug))
		}
		if logFileLevel.Level() > zapcore.Level(Debug) {
			logFileLevel.SetLevel(zapcore.Level(Debug))
		}
	})

	f.amplificationTracker.markAmplification()
}

func (f *factory) Revert() {
	f.revertLoggers()
	f.frozen.Store(false)
}

func (f *factory) revertLoggers() {
	if f.amplificationDisabled() || f.amplificationTracker.startedAmplification.isZero() {
		return
	}

	f.setLoggingCoreLogLevel(func(logDisplayLevel zap.AtomicLevel, logFileLevel zap.AtomicLevel, originalDisplayLevel zapcore.Level, originalLogFileLevel zapcore.Level) {
		logDisplayLevel.SetLevel(originalDisplayLevel)
		logFileLevel.SetLevel(originalLogFileLevel)
	})

	f.amplificationTracker.markRevertion()
}

func (f *factory) amplificationDisabled() bool {
	return !f.config.LoggingAutoAmplification || f.config.LoggingAutoAmplificationMaxDuration == 0
}

type amplificationDurationTracker struct {
	now                  func() time.Time
	startedAmplification atomicTime
	maxDuration          time.Duration
}

func (t *amplificationDurationTracker) isAmplificationDurationExhausted() bool {
	startedTime := t.startedAmplification.now()

	if startedTime.IsZero() {
		return false
	}

	elapsed := t.now().Sub(startedTime)

	return elapsed >= t.maxDuration
}

func (t *amplificationDurationTracker) markAmplification() {
	t.startedAmplification.setIfZero(t.now())
}

func (t *amplificationDurationTracker) markRevertion() {
	t.startedAmplification.setZero()
}

type atomicTime struct {
	v atomic.Int64
}

func (at *atomicTime) now() time.Time {
	now := at.v.Load()

	if now == 0 {
		return time.Time{}
	}

	return time.UnixMilli(now)
}

func (at *atomicTime) setZero() {
	at.v.Store(0)
}

func (at *atomicTime) isZero() bool {
	return at.v.Load() == 0
}

func (at *atomicTime) setIfZero(t2 time.Time) {
	at.v.CompareAndSwap(0, t2.UnixMilli())
}
