// Copyright 2017 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Package tracers is a manager for transaction tracing engines.
package tracers

import (
	ethtracers "github.com/ava-labs/libevm/eth/tracers"
)

// Context contains some contextual infos for a transaction execution that is not
// available from within the EVM object.
type Context = ethtracers.Context

// Tracer interface extends vm.EVMLogger and additionally
// allows collecting the tracing result.
type Tracer = ethtracers.Tracer

// DefaultDirectory is the collection of tracers bundled by default.
var DefaultDirectory = ethtracers.DefaultDirectory

// GetMemoryCopyPadded returns offset + size as a new slice.
// It zero-pads the slice if it extends beyond memory bounds.
var GetMemoryCopyPadded = ethtracers.GetMemoryCopyPadded
