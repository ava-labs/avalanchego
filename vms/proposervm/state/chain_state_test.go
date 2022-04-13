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

package state

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/chain4travel/caminogo/database"
	"github.com/chain4travel/caminogo/database/memdb"
	"github.com/chain4travel/caminogo/ids"
)

func testChainState(a *assert.Assertions, cs ChainState) {
	lastAccepted := ids.GenerateTestID()

	_, err := cs.GetLastAccepted()
	a.Equal(database.ErrNotFound, err)

	err = cs.SetLastAccepted(lastAccepted)
	a.NoError(err)

	err = cs.SetLastAccepted(lastAccepted)
	a.NoError(err)

	fetchedLastAccepted, err := cs.GetLastAccepted()
	a.NoError(err)
	a.Equal(lastAccepted, fetchedLastAccepted)

	fetchedLastAccepted, err = cs.GetLastAccepted()
	a.NoError(err)
	a.Equal(lastAccepted, fetchedLastAccepted)

	err = cs.DeleteLastAccepted()
	a.NoError(err)

	_, err = cs.GetLastAccepted()
	a.Equal(database.ErrNotFound, err)
}

func TestChainState(t *testing.T) {
	a := assert.New(t)

	db := memdb.New()
	cs := NewChainState(db)

	testChainState(a, cs)
}
