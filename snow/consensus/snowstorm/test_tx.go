// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

// TestTx is a useful test tx
type TestTx struct {
	choices.TestDecidable

	DependenciesV    []Tx
	DependenciesErrV error
	InputIDsV        []ids.ID
	WhitelistV       ids.Set
	WhitelistIsV     bool
	WhitelistErrV    error
	VerifyV          error
	BytesV           []byte
}

// Dependencies implements the Tx interface
func (t *TestTx) Dependencies() ([]Tx, error) { return t.DependenciesV, t.DependenciesErrV }

// InputIDs implements the Tx interface
func (t *TestTx) InputIDs() []ids.ID { return t.InputIDsV }

// Whitelist implements the Tx.Whitelister interface
func (t *TestTx) Whitelist() (ids.Set, bool, error) {
	return t.WhitelistV, t.WhitelistIsV, t.WhitelistErrV
}

// Verify implements the Tx interface
func (t *TestTx) Verify() error { return t.VerifyV }

// Bytes returns the bits
func (t *TestTx) Bytes() []byte { return t.BytesV }
