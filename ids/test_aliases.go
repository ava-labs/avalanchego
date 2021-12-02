// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"github.com/stretchr/testify/assert"
)

var AliasTests = []func(assert *assert.Assertions, r AliaserReader, w AliaserWriter){
	AliaserLookupErrorTest,
	AliaserLookupTest,
	AliaserAliasesEmptyTest,
	AliaserAliasesTest,
	AliaserPrimaryAliasTest,
	AliaserAliasClashTest,
	AliaserRemoveAliasTest,
}

func AliaserLookupErrorTest(assert *assert.Assertions, r AliaserReader, w AliaserWriter) {
	_, err := r.Lookup("Batman")
	assert.Error(err, "expected an error due to missing alias")
}

func AliaserLookupTest(assert *assert.Assertions, r AliaserReader, w AliaserWriter) {
	id := ID{'K', 'a', 't', 'e', ' ', 'K', 'a', 'n', 'e'}
	err := w.Alias(id, "Batwoman")
	assert.NoError(err)

	res, err := r.Lookup("Batwoman")
	assert.NoError(err)
	assert.Equal(id, res)
}

func AliaserAliasesEmptyTest(assert *assert.Assertions, r AliaserReader, w AliaserWriter) {
	id := ID{'J', 'a', 'm', 'e', 's', ' ', 'G', 'o', 'r', 'd', 'o', 'n'}

	aliases, err := r.Aliases(id)
	assert.NoError(err)
	assert.Empty(aliases)
}

func AliaserAliasesTest(assert *assert.Assertions, r AliaserReader, w AliaserWriter) {
	id := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	err := w.Alias(id, "Batman")
	assert.NoError(err)

	err = w.Alias(id, "Dark Knight")
	assert.NoError(err)

	aliases, err := r.Aliases(id)
	assert.NoError(err)

	expected := []string{"Batman", "Dark Knight"}
	assert.Equal(expected, aliases)
}

func AliaserPrimaryAliasTest(assert *assert.Assertions, r AliaserReader, w AliaserWriter) {
	id1 := ID{'J', 'a', 'm', 'e', 's', ' ', 'G', 'o', 'r', 'd', 'o', 'n'}
	id2 := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	err := w.Alias(id2, "Batman")
	assert.NoError(err)

	err = w.Alias(id2, "Dark Knight")
	assert.NoError(err)

	_, err = r.PrimaryAlias(id1)
	assert.Error(err)

	expected := "Batman"
	res, err := r.PrimaryAlias(id2)
	assert.NoError(err)
	assert.Equal(expected, res)
}

func AliaserAliasClashTest(assert *assert.Assertions, r AliaserReader, w AliaserWriter) {
	id1 := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	id2 := ID{'D', 'i', 'c', 'k', ' ', 'G', 'r', 'a', 'y', 's', 'o', 'n'}
	err := w.Alias(id1, "Batman")
	assert.NoError(err)

	err = w.Alias(id2, "Batman")
	assert.Error(err)
}

func AliaserRemoveAliasTest(assert *assert.Assertions, r AliaserReader, w AliaserWriter) {
	id1 := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	id2 := ID{'J', 'a', 'm', 'e', 's', ' ', 'G', 'o', 'r', 'd', 'o', 'n'}
	err := w.Alias(id1, "Batman")
	assert.NoError(err)

	err = w.Alias(id1, "Dark Knight")
	assert.NoError(err)

	w.RemoveAliases(id1)

	_, err = r.PrimaryAlias(id1)
	assert.Error(err)

	err = w.Alias(id2, "Batman")
	assert.NoError(err)

	err = w.Alias(id2, "Dark Knight")
	assert.NoError(err)

	err = w.Alias(id1, "Dark Night Rises")
	assert.NoError(err)
}
