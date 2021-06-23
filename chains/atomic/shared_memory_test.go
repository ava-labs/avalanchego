// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestSharedMemory(t *testing.T) {
	assert := assert.New(t)

	chainID0 := ids.GenerateTestID()
	chainID1 := ids.GenerateTestID()

	for _, test := range SharedMemoryTests {
		m := Memory{}
		baseDB := memdb.New()
		memoryDB := prefixdb.New([]byte{0}, baseDB)
		testDB := prefixdb.New([]byte{1}, baseDB)

		err := m.Initialize(logging.NoLog{}, memoryDB)
		assert.NoError(err)

		sm0 := m.NewSharedMemory(chainID0)
		sm1 := m.NewSharedMemory(chainID1)

		batchChainsAndInputs := make(map[ids.ID][]*Requests)

		byteArr := [][]byte{{0}, {1}, {2}}

		batchChainsAndInputs[chainID0] = append([]*Requests{}, &Requests{Remove, byteArr, []*Element{{
			Key:   []byte{0},
			Value: []byte{1},
		}}})

		batchChainsAndInputs[chainID1] = append([]*Requests{}, &Requests{Put, byteArr, []*Element{{
			Key:   []byte{0},
			Value: []byte{1},
		}}})

		test(t, chainID0, chainID1, sm0, sm1, testDB, batchChainsAndInputs)
	}
}
