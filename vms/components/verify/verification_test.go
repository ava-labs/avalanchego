// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package verify

import (
	"errors"
	"testing"
)

var errTest = errors.New("non-nil error")

type testVerifiable struct{ err error }

func (v testVerifiable) Verify() error { return v.err }

func TestAllNil(t *testing.T) {
	err := All(
		testVerifiable{},
		testVerifiable{},
	)
	if err != nil {
		t.Fatal(err)
	}
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
