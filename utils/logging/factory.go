// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

// Factory ...
type Factory interface {
	Make(name string) (Logger, error)
	MakeChain(chainID string) (Logger, error)
	MakeChainChild(chainID string, name string) (Logger, error)
	Close()
}

// factory ...
type factory struct {
	config Config

	loggers []Logger
}

// NewFactory ...
func NewFactory(config Config) Factory {
	return &factory{
		config: config,
	}
}

// Make ...
func (f *factory) Make(name string) (Logger, error) {
	config := f.config
	config.LoggerName = name
	l, err := New(config)
	if err == nil {
		f.loggers = append(f.loggers, l)
	}
	return l, err
}

// MakeChain ...
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

// MakeChainChild ...
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

// Close ...
func (f *factory) Close() {
	for _, log := range f.loggers {
		log.Stop()
	}
	f.loggers = nil
}
