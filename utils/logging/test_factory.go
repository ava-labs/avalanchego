// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

// NoFactory ...
type NoFactory struct{}

// Make ...
func (NoFactory) Make() (Logger, error) { return NoLog{}, nil }

// MakeChain ...
func (NoFactory) MakeChain(string, string) (Logger, error) { return NoLog{}, nil }

// MakeSubdir ...
func (NoFactory) MakeSubdir(string) (Logger, error) { return NoLog{}, nil }

// Close ...
func (NoFactory) Close() {}
