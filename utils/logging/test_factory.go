// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

// NoFactory ...
type NoFactory struct{}

// Make ...
func (NoFactory) Make(string) (Logger, error) { return NoLog{}, nil }

// MakeChain ...
func (NoFactory) MakeChain(string) (Logger, error) { return NoLog{}, nil }

// MakeChainChild ...
func (NoFactory) MakeChainChild(string, string) (Logger, error) { return NoLog{}, nil }

// Close ...
func (NoFactory) Close() {}

// SetLogLevel ...
func (NoFactory) SetLogLevel(name string, level Level) error { return nil }

// SetDisplayLevel ...
func (NoFactory) SetDisplayLevel(name string, level Level) error { return nil }

// GetLogLevel ...
func (NoFactory) GetLogLevel(name string) (Level, error) { return -1, nil }

// GetDisplayLevel ...
func (NoFactory) GetDisplayLevel(name string) (Level, error) { return -1, nil }

// GetNames ...
func (NoFactory) GetNames() []string { return nil }
