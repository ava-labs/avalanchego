// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"fmt"
	"sync"
)

var _ Factory = &factory{}

// Factory creates new instances of different types of Logger
type Factory interface {
	// Make creates a new logger with name [name]
	Make(name string) (Logger, error)

	// MakeChain creates a new logger to log the events of chain [chainID]
	MakeChain(chainID string) (Logger, error)

	// MakeChainChild creates a new sublogger for a [name] module of a chain [chainId]
	MakeChainChild(chainID string, name string) (Logger, error)

	// SetLogLevels sets log levels for all loggers in factory with given logger name, level pairs.
	SetLogLevel(name string, level Level) error

	// SetDisplayLevels sets log display levels for all loggers in factory with given logger name, level pairs.
	SetDisplayLevel(name string, level Level) error

	// GetLogLevels returns all log levels in factory as name, level pairs
	GetLogLevel(name string) (Level, error)

	// GetDisplayLevels returns all log display levels in factory as name, level pairs
	GetDisplayLevel(name string) (Level, error)

	// GetLoggerNames returns the names of all logs created by this factory
	GetLoggerNames() []string

	// Close stops and clears all of a Factory's instantiated loggers
	Close()
}

type factory struct {
	config Config
	lock   sync.RWMutex

	// For each logger created by this factory:
	// Logger name --> the logger.
	loggers map[string]Logger
}

// NewFactory returns a new instance of a Factory producing loggers configured with
// the values set in the [config] parameter
func NewFactory(config Config) Factory {
	return &factory{
		config:  config,
		loggers: make(map[string]Logger),
	}
}

// Assumes [f.lock] is held
func (f *factory) makeLogger(config Config) (Logger, error) {
	if _, ok := f.loggers[config.LoggerName]; ok {
		return nil, fmt.Errorf("logger with name %q already exists", config.LoggerName)
	}
	l, err := newLog(config)
	if err != nil {
		return nil, err
	}
	f.loggers[config.LoggerName] = l
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

func (f *factory) MakeChainChild(chainID string, name string) (Logger, error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	config := f.config
	config.MsgPrefix = chainID + " Chain"
	config.LoggerName = chainID + "." + name
	return f.makeLogger(config)
}

func (f *factory) SetLogLevel(name string, level Level) error {
	f.lock.RLock()
	defer f.lock.RUnlock()

	logger, ok := f.loggers[name]
	if !ok {
		return fmt.Errorf("logger with name %q not found", name)
	}
	logger.SetLogLevel(level)
	return nil
}

func (f *factory) SetDisplayLevel(name string, level Level) error {
	f.lock.RLock()
	defer f.lock.RUnlock()

	logger, ok := f.loggers[name]
	if !ok {
		return fmt.Errorf("logger with name %q not found", name)
	}
	logger.SetDisplayLevel(level)
	return nil
}

func (f *factory) GetLogLevel(name string) (Level, error) {
	f.lock.RLock()
	defer f.lock.RUnlock()

	logger, ok := f.loggers[name]
	if !ok {
		return -1, fmt.Errorf("logger with name %q not found", name)
	}
	return logger.GetLogLevel(), nil
}

func (f *factory) GetDisplayLevel(name string) (Level, error) {
	f.lock.RLock()
	defer f.lock.RUnlock()

	logger, ok := f.loggers[name]
	if !ok {
		return -1, fmt.Errorf("logger with name %q not found", name)
	}
	return logger.GetDisplayLevel(), nil
}

func (f *factory) GetLoggerNames() []string {
	f.lock.RLock()
	defer f.lock.RUnlock()

	names := make([]string, 0, len(f.loggers))
	for name := range f.loggers {
		names = append(names, name)
	}
	return names
}

func (f *factory) Close() {
	f.lock.Lock()
	defer f.lock.Unlock()

	for _, logger := range f.loggers {
		logger.Stop()
	}
	f.loggers = nil
}
