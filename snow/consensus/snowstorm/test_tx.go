// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

var _ Tx = &TestTx{}

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

func (t *TestTx) Dependencies() ([]Tx, error) { return t.DependenciesV, t.DependenciesErrV }
func (t *TestTx) InputIDs() []ids.ID          { return t.InputIDsV }
func (t *TestTx) Whitelist() (ids.Set, bool, error) {
	return t.WhitelistV, t.WhitelistIsV, t.WhitelistErrV
}
func (t *TestTx) Verify() error { return t.VerifyV }
func (t *TestTx) Bytes() []byte { return t.BytesV }
