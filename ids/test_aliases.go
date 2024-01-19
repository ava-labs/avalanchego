// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import "github.com/stretchr/testify/require"

var AliasTests = []func(require *require.Assertions, r AliaserReader, w AliaserWriter){
	AliaserLookupErrorTest,
	AliaserLookupTest,
	AliaserAliasesEmptyTest,
	AliaserAliasesTest,
	AliaserPrimaryAliasTest,
	AliaserAliasClashTest,
	AliaserRemoveAliasTest,
}

func AliaserLookupErrorTest(require *require.Assertions, r AliaserReader, _ AliaserWriter) {
	_, err := r.Lookup("Batman")
	// TODO: require error to be errNoIDWithAlias
	require.Error(err) //nolint:forbidigo // currently returns grpc errors too
}

func AliaserLookupTest(require *require.Assertions, r AliaserReader, w AliaserWriter) {
	id := ID{'K', 'a', 't', 'e', ' ', 'K', 'a', 'n', 'e'}
	require.NoError(w.Alias(id, "Batwoman"))

	res, err := r.Lookup("Batwoman")
	require.NoError(err)
	require.Equal(id, res)
}

func AliaserAliasesEmptyTest(require *require.Assertions, r AliaserReader, _ AliaserWriter) {
	id := ID{'J', 'a', 'm', 'e', 's', ' ', 'G', 'o', 'r', 'd', 'o', 'n'}

	aliases, err := r.Aliases(id)
	require.NoError(err)
	require.Empty(aliases)
}

func AliaserAliasesTest(require *require.Assertions, r AliaserReader, w AliaserWriter) {
	id := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}

	require.NoError(w.Alias(id, "Batman"))
	require.NoError(w.Alias(id, "Dark Knight"))

	aliases, err := r.Aliases(id)
	require.NoError(err)

	expected := []string{"Batman", "Dark Knight"}
	require.Equal(expected, aliases)
}

func AliaserPrimaryAliasTest(require *require.Assertions, r AliaserReader, w AliaserWriter) {
	id1 := ID{'J', 'a', 'm', 'e', 's', ' ', 'G', 'o', 'r', 'd', 'o', 'n'}
	id2 := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}

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

func AliaserAliasClashTest(require *require.Assertions, _ AliaserReader, w AliaserWriter) {
	id1 := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	id2 := ID{'D', 'i', 'c', 'k', ' ', 'G', 'r', 'a', 'y', 's', 'o', 'n'}

	require.NoError(w.Alias(id1, "Batman"))

	err := w.Alias(id2, "Batman")
	// TODO: require error to be errAliasAlreadyMapped
	require.Error(err) //nolint:forbidigo // currently returns grpc errors too
}

func AliaserRemoveAliasTest(require *require.Assertions, r AliaserReader, w AliaserWriter) {
	id1 := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	id2 := ID{'J', 'a', 'm', 'e', 's', ' ', 'G', 'o', 'r', 'd', 'o', 'n'}

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
