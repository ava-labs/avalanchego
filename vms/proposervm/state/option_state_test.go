// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/proposervm/option"
)

func TestOptionState(t *testing.T) {
	assert := assert.New(t)

	parentID := ids.ID{1}
	innerBlockBytes := []byte{3}
	opt, err := option.Build(parentID, innerBlockBytes)
	assert.NoError(err)

	db := memdb.New()

	os := NewOptionState(db)

	_, _, err = os.GetOption(opt.ID())
	assert.Equal(database.ErrNotFound, err)

	_, _, err = os.GetOption(opt.ID())
	assert.Equal(database.ErrNotFound, err)

	err = os.PutOption(opt, choices.Accepted)
	assert.NoError(err)

	fetchedOption, fetchedStatus, err := os.GetOption(opt.ID())
	assert.NoError(err)
	assert.Equal(choices.Accepted, fetchedStatus)
	assert.Equal(opt.Bytes(), fetchedOption.Bytes())

	os.WipeCache()

	fetchedOption, fetchedStatus, err = os.GetOption(opt.ID())
	assert.NoError(err)
	assert.Equal(choices.Accepted, fetchedStatus)
	assert.Equal(opt.Bytes(), fetchedOption.Bytes())

	err = os.DeleteOption(opt.ID())
	assert.NoError(err)

	_, _, err = os.GetOption(opt.ID())
	assert.Equal(database.ErrNotFound, err)
}
