// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"github.com/ava-labs/gecko/ids"
)

// NoFactory ...
type NoFactory struct{}

// Make ...
func (NoFactory) Make() (Logger, error) { return NoLog{}, nil }

// MakeChain ...
func (NoFactory) MakeChain(ids.ID, string) (Logger, error) { return NoLog{}, nil }

// MakeSubdir ...
func (NoFactory) MakeSubdir(string) (Logger, error) { return NoLog{}, nil }

// Close ...
func (NoFactory) Close() {}
