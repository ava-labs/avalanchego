// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids/idstest"

	. "github.com/ava-labs/avalanchego/ids"
)

func TestAliaser(t *testing.T) {
	idstest.RunAllAlias(t, func() (AliaserReader, AliaserWriter) {
		a := NewAliaser()
		return a, a
	})
}

func TestPrimaryAliasOrDefaultTest(t *testing.T) {
	require := require.New(t)
	aliaser := NewAliaser()
	id1 := ID{'J', 'a', 'm', 'e', 's', ' ', 'G', 'o', 'r', 'd', 'o', 'n'}
	id2 := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	require.NoError(aliaser.Alias(id2, "Batman"))

	require.NoError(aliaser.Alias(id2, "Dark Knight"))

	res := aliaser.PrimaryAliasOrDefault(id1)
	require.Equal(res, id1.String())

	expected := "Batman"
	require.Equal(expected, aliaser.PrimaryAliasOrDefault(id2))
}
