// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
	require := require.New(t)

	err := All(
		testVerifiable{},
		testVerifiable{},
	)
	require.NoError(err)
}

func TestAllError(t *testing.T) {
	err := All(
		testVerifiable{},
		testVerifiable{err: errTest},
	)
	if err == nil {
		t.Fatalf("Should have returned an error")
	}
}
