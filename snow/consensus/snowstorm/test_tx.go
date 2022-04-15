// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow/choices"
)

var _ Tx = &TestTx{}

// TestTx is a useful test tx
type TestTx struct {
	choices.TestDecidable

	DependenciesV    []Tx
	DependenciesErrV error
	InputIDsV        []ids.ID
	HasWhitelistV    bool
	WhitelistV       ids.Set
	WhitelistErrV    error
	VerifyV          error
	BytesV           []byte
}

func (t *TestTx) Dependencies() ([]Tx, error) { return t.DependenciesV, t.DependenciesErrV }
func (t *TestTx) InputIDs() []ids.ID          { return t.InputIDsV }
func (t *TestTx) HasWhitelist() bool          { return t.HasWhitelistV }
func (t *TestTx) Whitelist() (ids.Set, error) { return t.WhitelistV, t.WhitelistErrV }
func (t *TestTx) Verify() error               { return t.VerifyV }
func (t *TestTx) Bytes() []byte               { return t.BytesV }
