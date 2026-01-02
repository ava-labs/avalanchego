// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
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
	err := r.AddAlias("7", "4")
	require.ErrorIs(err, errAlreadyReserved)

	handler1 := &testHandler{}
	err = r.AddRouter("2", "", handler1)
	require.ErrorIs(err, errAlreadyReserved)
	require.NoError(r.AddRouter("5", "", handler1))

	handler, exists := r.routes["5"][""]
	require.True(exists)
	require.Equal(handler1, handler)

	require.NoError(r.AddAlias("5", "7"))

	handler, exists = r.routes["7"][""]
	require.True(exists)
	require.Equal(handler1, handler)

	handler, err = r.GetHandler("7", "")
	require.NoError(err)
	require.Equal(handler1, handler)
}

func TestBlock(t *testing.T) {
	require := require.New(t)
	r := newRouter()

	require.NoError(r.AddAlias("1", "1"))

	handler1 := &testHandler{}
	err := r.AddRouter("1", "", handler1)
	require.ErrorIs(err, errAlreadyReserved)
}
