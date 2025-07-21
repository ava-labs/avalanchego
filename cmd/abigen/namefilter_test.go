// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNameFilter(t *testing.T) {
	t.Parallel()
	_, err := newNameFilter("Foo")
	require.Error(t, err)
	_, err = newNameFilter("too/many:colons:Foo")
	require.Error(t, err)

	f, err := newNameFilter("a/path:A", "*:B", "c/path:*")
	require.NoError(t, err)

	for _, tt := range []struct {
		name  string
		match bool
	}{
		{"a/path:A", true},
		{"unknown/path:A", false},
		{"a/path:X", false},
		{"unknown/path:X", false},
		{"any/path:B", true},
		{"c/path:X", true},
		{"c/path:foo:B", false},
	} {
		match := f.Matches(tt.name)
		if tt.match {
			assert.True(t, match, "expected match")
		} else {
			assert.False(t, match, "expected no match")
		}
	}
}
