// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package leveldb

import (
	"fmt"
	"os"
	"testing"

	"github.com/ava-labs/avalanchego/database"
)

func TestInterface(t *testing.T) {
	for i, test := range database.Tests {
		folder := fmt.Sprintf("db%d", i)

		db, err := New(folder, 0, 0, 0)
		if err != nil {
			t.Fatalf("leveldb.New(%s, 0, 0) errored with %s", folder, err)
		}
		defer os.RemoveAll(folder)
		defer db.Close()

		test(t, db)
	}
}
