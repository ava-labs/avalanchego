// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package codectest provides a test suite for testing functionality related to
// IDs.
package idstest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

// An AliasTest couples a test in the Aliaser suite with a human-readable name.
type AliasTest struct {
	Name string
	Test func(testing.TB, ids.AliaserReader, ids.AliaserWriter)
}

// Run runs the test on the Aliaser{Reader+Writer} pair.
func (tt *AliasTest) Run(t *testing.T, r ids.AliaserReader, w ids.AliaserWriter) {
	t.Run(tt.Name, func(t *testing.T) {
		tt.Test(t, r, w)
	})
}

// RunAllAlias runs all [AliasTests], constructing a new GeneralCodec for each.
func RunAllAlias(t *testing.T, ctor func() (ids.AliaserReader, ids.AliaserWriter)) {
	for _, tt := range AliasTests {
		r, w := ctor()
		tt.Run(t, r, w)
	}
}

var AliasTests = []AliasTest{
	{"Lookup Error}", TestAliaserLookupError},
	{"Lookup}", TestAliaserLookup},
	{"Aliases Empty}", TestAliaserAliasesEmpty},
	{"Aliases}", TestAliaserAliases},
	{"Primary Alias}", TestAliaserPrimaryAlias},
	{"Alias Clash}", TestAliaserAliasClash},
	{"Remove Alias}", TestAliaserRemoveAlias},
}

func TestAliaserLookupError(tb testing.TB, r ids.AliaserReader, _ ids.AliaserWriter) {
	require := require.New(tb)
	_, err := r.Lookup("Batman")
	// TODO: require error to be errNoIDWithAlias
	require.Error(err) //nolint:forbidigo // currently returns grpc errors too
}

func TestAliaserLookup(tb testing.TB, r ids.AliaserReader, w ids.AliaserWriter) {
	require := require.New(tb)
	id := ids.ID{'K', 'a', 't', 'e', ' ', 'K', 'a', 'n', 'e'}
	require.NoError(w.Alias(id, "Batwoman"))

	res, err := r.Lookup("Batwoman")
	require.NoError(err)
	require.Equal(id, res)
}

func TestAliaserAliasesEmpty(tb testing.TB, r ids.AliaserReader, _ ids.AliaserWriter) {
	require := require.New(tb)
	id := ids.ID{'J', 'a', 'm', 'e', 's', ' ', 'G', 'o', 'r', 'd', 'o', 'n'}

	aliases, err := r.Aliases(id)
	require.NoError(err)
	require.Empty(aliases)
}

func TestAliaserAliases(tb testing.TB, r ids.AliaserReader, w ids.AliaserWriter) {
	require := require.New(tb)
	id := ids.ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}

	require.NoError(w.Alias(id, "Batman"))
	require.NoError(w.Alias(id, "Dark Knight"))

	aliases, err := r.Aliases(id)
	require.NoError(err)

	expected := []string{"Batman", "Dark Knight"}
	require.Equal(expected, aliases)
}

func TestAliaserPrimaryAlias(tb testing.TB, r ids.AliaserReader, w ids.AliaserWriter) {
	require := require.New(tb)
	id1 := ids.ID{'J', 'a', 'm', 'e', 's', ' ', 'G', 'o', 'r', 'd', 'o', 'n'}
	id2 := ids.ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}

	require.NoError(w.Alias(id2, "Batman"))
	require.NoError(w.Alias(id2, "Dark Knight"))

	_, err := r.PrimaryAlias(id1)
	// TODO: require error to be errNoAliasForID
	require.Error(err) //nolint:forbidigo // currently returns grpc errors too

	expected := "Batman"
	res, err := r.PrimaryAlias(id2)
	require.NoError(err)
	require.Equal(expected, res)
}

func TestAliaserAliasClash(tb testing.TB, _ ids.AliaserReader, w ids.AliaserWriter) {
	require := require.New(tb)
	id1 := ids.ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	id2 := ids.ID{'D', 'i', 'c', 'k', ' ', 'G', 'r', 'a', 'y', 's', 'o', 'n'}

	require.NoError(w.Alias(id1, "Batman"))

	err := w.Alias(id2, "Batman")
	// TODO: require error to be errAliasAlreadyMapped
	require.Error(err) //nolint:forbidigo // currently returns grpc errors too
}

func TestAliaserRemoveAlias(tb testing.TB, r ids.AliaserReader, w ids.AliaserWriter) {
	require := require.New(tb)
	id1 := ids.ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	id2 := ids.ID{'J', 'a', 'm', 'e', 's', ' ', 'G', 'o', 'r', 'd', 'o', 'n'}

	require.NoError(w.Alias(id1, "Batman"))
	require.NoError(w.Alias(id1, "Dark Knight"))

	w.RemoveAliases(id1)

	_, err := r.PrimaryAlias(id1)
	// TODO: require error to be errNoAliasForID
	require.Error(err) //nolint:forbidigo // currently returns grpc errors too

	require.NoError(w.Alias(id2, "Batman"))
	require.NoError(w.Alias(id2, "Dark Knight"))
	require.NoError(w.Alias(id1, "Dark Night Rises"))
}
