// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import "fmt"

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

	// GetDisplayLevels returns all log display levels in factory as name, level pairs
	GetNames() []string

	// Close stops and clears all of a Factory's instantiated loggers
	Close()
}

// factory implements the Factory interface
type factory struct {
	config Config

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

func (f *factory) makeLogger(config Config) (Logger, error) {
	if _, ok := f.loggers[config.LoggerName]; ok {
		return nil, fmt.Errorf("logger with name: %s already exists", config.LoggerName)
	}
	l, err := New(config)
	if err == nil {
		f.loggers[config.LoggerName] = l
	}
	return l, err
}

// Make implements the Factory interface
func (f *factory) Make(name string) (Logger, error) {
	config := f.config
	config.LoggerName = name
	return f.makeLogger(config)
}

// MakeChain implements the Factory interface
func (f *factory) MakeChain(chainID string) (Logger, error) {
	config := f.config
	config.MsgPrefix = chainID + " Chain"
	config.LoggerName = chainID
	return f.makeLogger(config)
}

// MakeChainChild implements the Factory interface
func (f *factory) MakeChainChild(chainID string, name string) (Logger, error) {
	config := f.config
	config.MsgPrefix = chainID + " Chain"
	config.LoggerName = chainID + "." + name
	return f.makeLogger(config)
}

// SetLogLevels implements the Factory interface
func (f *factory) SetLogLevel(name string, level Level) error {
	if _, ok := f.loggers[name]; !ok {
		return fmt.Errorf("logger with name %v not found", name)
	}
	f.loggers[name].SetLogLevel(level)
	return nil
}

// SetLogLevels implements the Factory interface
func (f *factory) SetDisplayLevel(name string, level Level) error {
	if _, ok := f.loggers[name]; !ok {
		return fmt.Errorf("logger with name %v not found", name)
	}
	f.loggers[name].SetDisplayLevel(level)
	return nil
}

// GetLogLevels implements the Factory interface
func (f *factory) GetLogLevel(name string) (Level, error) {
	if _, ok := f.loggers[name]; !ok {
		return -1, fmt.Errorf("logger with name %v not found", name)
	}

	return f.loggers[name].GetLogLevel(), nil
}

// GetLogLevels implements the Factory interface
func (f *factory) GetDisplayLevel(name string) (Level, error) {
	if _, ok := f.loggers[name]; !ok {
		return -1, fmt.Errorf("logger with name %v not found", name)
	}

	return f.loggers[name].GetDisplayLevel(), nil
}

// GetDisplayLevels implements the Factory interface
func (f *factory) GetNames() []string {
	names := make([]string, 0, len(f.loggers))
	for name := range f.loggers {
		names = append(names, name)
	}
	return names
}

// Close implements the Factory interface
func (f *factory) Close() {
	for _, log := range f.loggers {
		log.Stop()
	}
	f.loggers = nil
}
