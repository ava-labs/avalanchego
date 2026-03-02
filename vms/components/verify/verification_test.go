// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package verify

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

var errTest = errors.New("non-nil error")

type testVerifiable struct{ err error }

func (v testVerifiable) Verify() error {
	return v.err
}

func TestAllNil(t *testing.T) {
	require.NoError(t, All(
		testVerifiable{},
		testVerifiable{},
	))
}

func TestAllError(t *testing.T) {
	err := All(
		testVerifiable{},
		testVerifiable{err: errTest},
	)
	require.ErrorIs(t, err, errTest)
}
