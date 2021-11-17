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
