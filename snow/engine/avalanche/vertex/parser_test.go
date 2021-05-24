package vertex

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/assert"
)

func TestParseInvalid(t *testing.T) {
	vtxBytes := []byte{}
	_, err := Parse(vtxBytes)
	assert.Error(t, err, "parse on an invalid vertex should have errored")
}

func TestParseValid(t *testing.T) {
	chainID := ids.ID{1}
	height := uint64(2)
	epoch := uint32(0)
	parentIDs := []ids.ID{{4}, {5}}
	txs := [][]byte{{6}, {7}}
	restrictions := []ids.ID(nil)
	vtx, err := Build(
		chainID,
		height,
		epoch,
		parentIDs,
		txs,
		restrictions,
	)
	assert.NoError(t, err)

	vtxBytes := vtx.Bytes()
	parsedVtx, err := Parse(vtxBytes)
	assert.NoError(t, err)
	assert.Equal(t, vtx, parsedVtx)
}
