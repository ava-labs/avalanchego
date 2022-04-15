// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"testing"

	"github.com/chain4travel/caminogo/utils/units"
	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	var (
		assert          = assert.New(t)
		maxN     uint64 = 10000
		p               = 0.1
		maxBytes uint64 = 1 * units.MiB // 1 MiB
	)
	f, err := New(maxN, p, maxBytes)
	assert.NoError(err)
	assert.NotNil(f)

	f.Add([]byte("hello"))

	checked := f.Check([]byte("hello"))
	assert.True(checked, "should have contained the key")

	checked = f.Check([]byte("bye"))
	assert.False(checked, "shouldn't have contained the key")
}
