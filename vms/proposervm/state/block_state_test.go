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
	"github.com/ava-labs/avalanchego/vms/proposervm/option"
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

func TestOptionState(t *testing.T) {
	assert := assert.New(t)

	parentID := ids.ID{1}
	innerBlockBytes := []byte{3}
	opt, err := option.Build(parentID, innerBlockBytes)
	assert.NoError(err)

	db := memdb.New()

	bs := NewBlockState(db)

	_, _, err = bs.GetOption(opt.ID())
	assert.Equal(database.ErrNotFound, err)

	_, _, err = bs.GetOption(opt.ID())
	assert.Equal(database.ErrNotFound, err)

	err = bs.PutOption(opt, choices.Accepted)
	assert.NoError(err)

	fetchedOption, fetchedStatus, err := bs.GetOption(opt.ID())
	assert.NoError(err)
	assert.Equal(choices.Accepted, fetchedStatus)
	assert.Equal(opt.Bytes(), fetchedOption.Bytes())

	bs.WipeCache()

	fetchedOption, fetchedStatus, err = bs.GetOption(opt.ID())
	assert.NoError(err)
	assert.Equal(choices.Accepted, fetchedStatus)
	assert.Equal(opt.Bytes(), fetchedOption.Bytes())

	err = bs.DeleteOption(opt.ID())
	assert.NoError(err)

	_, _, err = bs.GetOption(opt.ID())
	assert.Equal(database.ErrNotFound, err)
}

func TestBlocksAndOptionsState(t *testing.T) {
	// build block and option
	assert := assert.New(t)

	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}

	tlsCert, err := staking.NewTLSCert()
	assert.NoError(err)

	cert := tlsCert.Leaf
	key := tlsCert.PrivateKey.(crypto.Signer)

	blk, err := block.Build(parentID, timestamp, pChainHeight, cert, innerBlockBytes, key)
	assert.NoError(err)

	opt, err := option.Build(parentID, innerBlockBytes)
	assert.NoError(err)

	// store them together
	db := memdb.New()
	bs := NewBlockState(db)

	err = bs.PutBlock(blk, choices.Accepted)
	assert.NoError(err)

	err = bs.PutOption(opt, choices.Accepted)
	assert.NoError(err)

	// pick them the right way
	fetchedBlock, fetchedStatus, err := bs.GetBlock(blk.ID())
	assert.NoError(err)
	assert.Equal(choices.Accepted, fetchedStatus)
	assert.Equal(blk.Bytes(), fetchedBlock.Bytes())

	fetchedOption, fetchedStatus, err := bs.GetOption(opt.ID())
	assert.NoError(err)
	assert.Equal(choices.Accepted, fetchedStatus)
	assert.Equal(opt.Bytes(), fetchedOption.Bytes())

	// attempt to mix picking them
	_, _, err = bs.GetOption(blk.ID())
	assert.Equal(err, database.ErrNotFound)

	_, _, err = bs.GetBlock(opt.ID())
	assert.Equal(err, database.ErrNotFound)

	// wipe cache and check you stil cannot mix them ...
	bs.WipeCache()

	_, _, err = bs.GetOption(blk.ID())
	assert.False(err == nil)

	_, _, err = bs.GetBlock(opt.ID())
	assert.False(err == nil)

	// but you can retrieve them in the right way
	fetchedBlock, fetchedStatus, err = bs.GetBlock(blk.ID())
	assert.NoError(err)
	assert.Equal(choices.Accepted, fetchedStatus)
	assert.Equal(blk.Bytes(), fetchedBlock.Bytes())

	fetchedOption, fetchedStatus, err = bs.GetOption(opt.ID())
	assert.NoError(err)
	assert.Equal(choices.Accepted, fetchedStatus)
	assert.Equal(opt.Bytes(), fetchedOption.Bytes())
}
