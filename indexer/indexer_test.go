package indexer

import (
	"io"
	"sync"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/triggers"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/assert"
)

type apiServerMock struct {
	timesCalled int
}

func (a *apiServerMock) AddRoute(_ *common.HTTPHandler, _ *sync.RWMutex, _, _ string, _ io.Writer) error {
	a.timesCalled++
	return nil
}

func TestIndexer(t *testing.T) {
	assert := assert.New(t)
	ed := &triggers.EventDispatcher{}
	ed.Initialize(logging.NoLog{})
	config := Config{
		IndexingEnabled:        true,
		AllowIncompleteIndex:   false,
		Log:                    logging.NoLog{},
		Name:                   "test",
		Db:                     memdb.New(),
		EventDispatcher:        ed,
		InitiallyIndexedChains: ids.Set{},
		ChainLookupF:           func(string) (ids.ID, error) { return ids.ID{}, nil },
		APIServer:              &apiServerMock{},
	}

	// Create indexer and make sure its state is right
	idxrIntf, err := NewIndexer(config)
	assert.NoError(err)
	idxr, ok := idxrIntf.(*indexer)
	assert.True(ok)
	assert.Equal(config.Name, idxr.name)
	assert.NotNil(idxr.log)
	assert.NotNil(idxr.db)
	assert.NotNil(idxr.indexedChains)
	assert.False(idxr.allowIncompleteIndex)
	assert.NotNil(idxr.chainLookup)
	assert.NotNil(idxr.codec)
	assert.NotNil(idxr.chainToIndex)
	assert.False(idxr.hasRunBefore)
	assert.Equal(1, config.APIServer.(*apiServerMock).timesCalled)
}
