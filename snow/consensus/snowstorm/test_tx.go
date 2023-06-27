// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/choices"
)

var _ Tx = (*TestTx)(nil)

// TestTx is a useful test tx
type TestTx struct {
	choices.TestDecidable

	DependenciesV    []Tx
	DependenciesErrV error
	VerifyV          error
	BytesV           []byte
}

func (t *TestTx) Dependencies() ([]Tx, error) {
	return t.DependenciesV, t.DependenciesErrV
}

func (t *TestTx) Verify(context.Context) error {
	return t.VerifyV
}

func (t *TestTx) Bytes() []byte {
	return t.BytesV
}
