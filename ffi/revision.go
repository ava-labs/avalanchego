// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// Package ffi provides a Go wrapper around the [Firewood] database.
//
// [Firewood]: https://github.com/ava-labs/firewood
package ffi

// #include <stdlib.h>
// #include "firewood.h"
import "C"

import (
	"errors"
	"fmt"
)

var (
	errRevisionNotFound  = errors.New("revision not found")
	errInvalidRootLength = fmt.Errorf("root hash must be %d bytes", RootLength)
)

type Revision struct {
	database *Database
	// The revision root
	root []byte
}

func (r *Revision) Get(key []byte) ([]byte, error) {
	return r.database.GetFromRoot(r.root, key)
}
