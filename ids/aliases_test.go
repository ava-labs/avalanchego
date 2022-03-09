// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAliaser(t *testing.T) {
	assert := assert.New(t)
	for _, test := range AliasTests {
		aliaser := NewAliaser()
		test(assert, aliaser, aliaser)
	}
}

func TestPrimaryAliasOrDefaultTest(t *testing.T) {
	assert := assert.New(t)
	aliaser := NewAliaser()
	id1 := ID{'J', 'a', 'm', 'e', 's', ' ', 'G', 'o', 'r', 'd', 'o', 'n'}
	id2 := ID{'B', 'r', 'u', 'c', 'e', ' ', 'W', 'a', 'y', 'n', 'e'}
	err := aliaser.Alias(id2, "Batman")
	assert.NoError(err)

	err = aliaser.Alias(id2, "Dark Knight")
	assert.NoError(err)

	res := aliaser.PrimaryAliasOrDefault(id1)
	assert.Equal(res, id1.String())

	expected := "Batman"
	res = aliaser.PrimaryAliasOrDefault(id2)
	assert.NoError(err)
	assert.Equal(expected, res)
}
