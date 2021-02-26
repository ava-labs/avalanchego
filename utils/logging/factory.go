// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

// Factory creates new instances of different types of Logger
type Factory interface {
	// Make creates a new logger with name [name]
	Make(name string) (Logger, error)
	// MakeChain creates a new logger to log the events of chain [chainID]
	MakeChain(chainID string) (Logger, error)
	// MakeChainChild creates a new sublogger for a [name] module of a chain [chainId]
	MakeChainChild(chainID string, name string) (Logger, error)
	// Close stops and clears all of a Factory's instanciated loggers
	Close()
}

// factory implements the Factory interface
type factory struct {
	config Config

	loggers []Logger
}

// NewFactory returns a new instance of a Factory producing loggers configured with
// the values set in the [config] parameter
func NewFactory(config Config) Factory {
	return &factory{
		config: config,
	}
}

// Make implements the Factory interface
func (f *factory) Make(name string) (Logger, error) {
	config := f.config
	config.LoggerName = name
	l, err := New(config)
	if err == nil {
		f.loggers = append(f.loggers, l)
	}
	return l, err
}

// MakeChain implements the Factory interface
func (f *factory) MakeChain(chainID string) (Logger, error) {
	config := f.config
	config.MsgPrefix = chainID + " Chain"
	config.LoggerName = chainID
	log, err := New(config)
	if err == nil {
		f.loggers = append(f.loggers, log)
	}
	return log, err
}

// MakeChainChild implements the Factory interface
func (f *factory) MakeChainChild(chainID string, name string) (Logger, error) {
	config := f.config
	config.MsgPrefix = chainID + " Chain"
	config.LoggerName = chainID + "." + name
	log, err := New(config)
	if err == nil {
		f.loggers = append(f.loggers, log)
	}
	return log, err
}

// Close implements the Factory interface
func (f *factory) Close() {
	for _, log := range f.loggers {
		log.Stop()
	}
	f.loggers = nil
}
