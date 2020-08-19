// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"bytes"
	"testing"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/database/prefixdb"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/utils/logging"
)

func TestWriteAll(t *testing.T) {
	baseDB := memdb.New()
	prefixedDBChain := prefixdb.New([]byte{0}, baseDB)
	prefixedDBSharedMemory := prefixdb.New([]byte{1}, baseDB)

	m := Memory{}
	m.Initialize(logging.NoLog{}, prefixedDBSharedMemory)

	sharedID := m.sharedID(blockchainID0, blockchainID1)

	sharedDB := m.GetDatabase(sharedID)

	writeDB0 := versiondb.New(prefixedDBChain)
	writeDB1 := versiondb.New(sharedDB)
	defer m.ReleaseDatabase(sharedID)

	if err := writeDB0.Put([]byte{1}, []byte{2}); err != nil {
		t.Fatal(err)
	}
	if err := writeDB1.Put([]byte{2}, []byte{3}); err != nil {
		t.Fatal(err)
	}

	batch0, err := writeDB0.CommitBatch()
	if err != nil {
		t.Fatal(err)
	}
	batch1, err := writeDB1.CommitBatch()
	if err != nil {
		t.Fatal(err)
	}

	if err := WriteAll(batch0, batch1); err != nil {
		t.Fatal(err)
	}

	if value, err := prefixedDBChain.Get([]byte{1}); err != nil {
		t.Fatal(err)
	} else if !bytes.Equal(value, []byte{2}) {
		t.Fatalf("database.Get Returned: 0x%x ; Expected: 0x%x", value, []byte{2})
	} else if value, err := sharedDB.Get([]byte{2}); err != nil {
		t.Fatal(err)
	} else if !bytes.Equal(value, []byte{3}) {
		t.Fatalf("database.Get Returned: 0x%x ; Expected: 0x%x", value, []byte{3})
	}
}
