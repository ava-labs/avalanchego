// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
)

func TestChainState(t *testing.T) {
	assert := assert.New(t)

	lastAccepted := ids.GenerateTestID()

	db := memdb.New()

	cs := NewChainState(db)

	_, err := cs.GetLastAccepted()
	assert.Equal(database.ErrNotFound, err)

	err = cs.SetLastAccepted(lastAccepted)
	assert.NoError(err)

	err = cs.SetLastAccepted(lastAccepted)
	assert.NoError(err)

	cs = NewChainState(db)

	fetchedLastAccepted, err := cs.GetLastAccepted()
	assert.NoError(err)
	assert.Equal(lastAccepted, fetchedLastAccepted)

	fetchedLastAccepted, err = cs.GetLastAccepted()
	assert.NoError(err)
	assert.Equal(lastAccepted, fetchedLastAccepted)
}
