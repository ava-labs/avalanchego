// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"fmt"
	"os"
	"path"
	"sync"

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
}

type logWrapper struct {
	logger       Logger
	displayLevel zap.AtomicLevel
	logLevel     zap.AtomicLevel
}

type factory struct {
	config Config
	lock   sync.RWMutex

	// For each logger created by this factory:
	// Logger name --> the logger.
	loggers map[string]logWrapper
}

// NewFactory returns a new instance of a Factory producing loggers configured with
// the values set in the [config] parameter
func NewFactory(config Config) Factory {
	return &factory{
		config:  config,
		loggers: make(map[string]logWrapper),
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
	f.loggers[config.LoggerName] = logWrapper{
		logger:       l,
		displayLevel: consoleCore.AtomicLevel,
		logLevel:     fileCore.AtomicLevel,
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
