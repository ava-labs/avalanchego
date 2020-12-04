package vertex

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/assert"
)

func TestBuildInvalid(t *testing.T) {
	chainID := ids.ID{1}
	height := uint64(2)
	epoch := uint32(0)
	parentIDs := []ids.ID{{4}, {5}}
	txs := [][]byte{{7}, {6}}
	restrictions := []ids.ID{{8}, {9}}
	_, err := Build(
		chainID,
		height,
		epoch,
		parentIDs,
		txs,
		restrictions,
	)
	assert.Error(t, err, "Build should have errored, but didn't")
}

func TestBuildValid(t *testing.T) {
	chainID := ids.ID{1}
	height := uint64(2)
	epoch := uint32(0)
	parentIDs := []ids.ID{{4}, {5}}
	txs := [][]byte{{7}, {6}}
	restrictions := []ids.ID{}
	vtx, err := Build(
		chainID,
		height,
		epoch,
		parentIDs,
		txs,
		restrictions,
	)
	assert.NoError(t, err)
	assert.Equal(t, chainID, vtx.ChainID())
	assert.Equal(t, height, vtx.Height())
	assert.Equal(t, epoch, vtx.Epoch())
	assert.Equal(t, parentIDs, vtx.ParentIDs())
	assert.Equal(t, txs, vtx.Txs())
	assert.Equal(t, restrictions, vtx.Restrictions())
}
