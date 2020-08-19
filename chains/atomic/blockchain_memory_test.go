// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"bytes"
	"testing"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/utils/logging"
)

func TestBlockchainMemory(t *testing.T) {
	m := Memory{}
	m.Initialize(logging.NoLog{}, memdb.New())

	bsm0 := m.NewBlockchainMemory(blockchainID0)
	bsm1 := m.NewBlockchainMemory(blockchainID1)

	sharedDB0 := bsm0.GetDatabase(blockchainID1)
	if err := sharedDB0.Put([]byte{1}, []byte{2}); err != nil {
		t.Fatal(err)
	}
	bsm0.ReleaseDatabase(blockchainID1)

	sharedDB1 := bsm1.GetDatabase(blockchainID0)
	if value, err := sharedDB1.Get([]byte{1}); err != nil {
		t.Fatal(err)
	} else if !bytes.Equal(value, []byte{2}) {
		t.Fatalf("database.Get Returned: 0x%x ; Expected: 0x%x", value, []byte{2})
	}
	bsm1.ReleaseDatabase(blockchainID0)
}
