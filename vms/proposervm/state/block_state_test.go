// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"crypto"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

func TestBlockState(t *testing.T) {
	assert := assert.New(t)

	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}

	tlsCert, err := staking.NewTLSCert()
	assert.NoError(err)

	cert := tlsCert.Leaf
	key := tlsCert.PrivateKey.(crypto.Signer)

	b, err := block.Build(parentID, timestamp, pChainHeight, cert, innerBlockBytes, key)
	assert.NoError(err)

	db := memdb.New()

	bs := NewBlockState(db)

	_, _, err = bs.GetBlock(b.ID())
	assert.Equal(database.ErrNotFound, err)

	_, _, err = bs.GetBlock(b.ID())
	assert.Equal(database.ErrNotFound, err)

	err = bs.PutBlock(b, choices.Accepted)
	assert.NoError(err)

	fetchedBlock, fetchedStatus, err := bs.GetBlock(b.ID())
	assert.NoError(err)
	assert.Equal(choices.Accepted, fetchedStatus)
	assert.Equal(b.Bytes(), fetchedBlock.Bytes())

	bs.WipeCache()

	fetchedBlock, fetchedStatus, err = bs.GetBlock(b.ID())
	assert.NoError(err)
	assert.Equal(choices.Accepted, fetchedStatus)
	assert.Equal(b.Bytes(), fetchedBlock.Bytes())

	err = bs.DeleteBlock(b.ID())
	assert.NoError(err)

	_, _, err = bs.GetBlock(b.ID())
	assert.Equal(database.ErrNotFound, err)
}
