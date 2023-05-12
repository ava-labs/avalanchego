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

	err := r.AddAlias("1", "2", "3")
	require.NoError(err)
	err = r.AddAlias("1", "4")
	require.NoError(err)
	err = r.AddAlias("5", "1")
	require.NoError(err)
	err = r.AddAlias("3", "6")
	require.NoError(err)
	err = r.AddAlias("7", "4")
	require.ErrorIs(err, errAlreadyReserved)

	handler1 := &testHandler{}
	err = r.AddRouter("2", "", handler1)
	require.ErrorIs(err, errAlreadyReserved)
	err = r.AddRouter("5", "", handler1)
	require.NoError(err)

	handler, exists := r.routes["5"][""]
	require.True(exists)
	require.Equal(handler1, handler)

	err = r.AddAlias("5", "7")
	require.NoError(err)

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

	err := r.AddAlias("1", "1")
	require.NoError(err)

	handler1 := &testHandler{}
	err = r.AddRouter("1", "", handler1)
	require.ErrorIs(err, errAlreadyReserved)
}
