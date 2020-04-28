// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"path"

	"github.com/ava-labs/gecko/ids"
)

// Factory ...
type Factory interface {
	Make() (Logger, error)
	MakeChain(chainID ids.ID, subdir string) (Logger, error)
	MakeSubdir(subdir string) (Logger, error)
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
func (f *factory) Make() (Logger, error) {
	l, err := New(f.config)
	if err == nil {
		f.loggers = append(f.loggers, l)
	}
	return l, err
}

// MakeChain ...
func (f *factory) MakeChain(chainID ids.ID, subdir string) (Logger, error) {
	config := f.config
	config.MsgPrefix = "chain " + chainID.String()
	config.Directory = path.Join(config.Directory, "chain", chainID.String(), subdir)

	log, err := New(config)
	if err == nil {
		f.loggers = append(f.loggers, log)
	}
	return log, err
}

// MakeSubdir ...
func (f *factory) MakeSubdir(subdir string) (Logger, error) {
	config := f.config
	config.Directory = path.Join(config.Directory, subdir)

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
