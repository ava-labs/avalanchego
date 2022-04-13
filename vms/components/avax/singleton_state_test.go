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

package avax

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/chain4travel/caminogo/database/memdb"
)

func TestSingletonState(t *testing.T) {
	assert := assert.New(t)

	db := memdb.New()
	s := NewSingletonState(db)

	isInitialized, err := s.IsInitialized()
	assert.NoError(err)
	assert.False(isInitialized)

	err = s.SetInitialized()
	assert.NoError(err)

	isInitialized, err = s.IsInitialized()
	assert.NoError(err)
	assert.True(isInitialized)
}
