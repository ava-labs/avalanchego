// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAliaser(t *testing.T) {
	require := require.New(t)
	for _, test := range AliasTests {
		aliaser := NewAliaser()
		test(require, aliaser, aliaser)
	}
}

func TestPrimaryAliasOrDefaultTest(t *testing.T) {
	require := require.New(t)
	aliaser := NewAliaser()
	id1 := ID{'J', 'a', 'm', 'e', 's', ' ', 'G', 'o', 'r', 'd', 'o', 'n'}
	id2 := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	err := aliaser.Alias(id2, "Batman")
	require.NoError(err)

	err = aliaser.Alias(id2, "Dark Knight")
	require.NoError(err)

	res := aliaser.PrimaryAliasOrDefault(id1)
	require.Equal(res, id1.String())

	expected := "Batman"
	res = aliaser.PrimaryAliasOrDefault(id2)
	require.NoError(err)
	require.Equal(expected, res)
}
