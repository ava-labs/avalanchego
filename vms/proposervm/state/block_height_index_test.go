// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
)

func TestHeightIndexRequiredResets(t *testing.T) {
	a := assert.New(t)

	db := memdb.New()
	vdb := versiondb.New(db)
	s := New(vdb)
	required, err := s.GetIndexResetRequired()
	a.NoError(err)
	a.True(required)
	err = s.ResetHeightIndex()
	a.NoError(err)
	required, err = s.GetIndexResetRequired()
	a.NoError(err)
	a.False(required)
}
