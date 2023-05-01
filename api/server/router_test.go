// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type testHandler struct{ called bool }

func (t *testHandler) ServeHTTP(_ http.ResponseWriter, _ *http.Request) {
	t.called = true
}

func TestAliasing(t *testing.T) {
	require := require.New(t)

	r := newRouter()

	require.NoError(r.AddAlias("1", "2", "3"))
	require.NoError(r.AddAlias("1", "4"))
	require.NoError(r.AddAlias("5", "1"))
	require.NoError(r.AddAlias("3", "6"))
	if err := r.AddAlias("7", "4"); err == nil {
		t.Fatalf("Already reserved %s", "4")
	}

	handler1 := &testHandler{}
	if err := r.AddRouter("2", "", handler1); err == nil {
		t.Fatalf("Already reserved %s", "2")
	}
	require.NoError(r.AddRouter("5", "", handler1))
	if handler, exists := r.routes["5"][""]; !exists {
		t.Fatalf("Should have added %s", "5")
	} else if handler != handler1 {
		t.Fatalf("Registered unknown handler")
	}

	require.NoError(r.AddAlias("5", "7"))

	if handler, exists := r.routes["7"][""]; !exists {
		t.Fatalf("Should have added %s", "7")
	} else if handler != handler1 {
		t.Fatalf("Registered unknown handler")
	}

	if handler, err := r.GetHandler("7", ""); err != nil {
		t.Fatalf("Should have added %s", "7")
	} else if handler != handler1 {
		t.Fatalf("Registered unknown handler")
	}
}

func TestBlock(t *testing.T) {
	require := require.New(t)
	r := newRouter()

	require.NoError(r.AddAlias("1", "1"))

	handler1 := &testHandler{}
	if err := r.AddRouter("1", "", handler1); err == nil {
		t.Fatalf("Permanently locked %s", "1")
	}
}
