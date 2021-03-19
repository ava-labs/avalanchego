// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package leveldb

import (
	"fmt"
	"path"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestInterface(t *testing.T) {
	for i, test := range database.Tests {
		tempDir := t.TempDir()
		folder := path.Join(tempDir, fmt.Sprintf("db%d", i))

		db, err := New(folder, logging.NoLog{}, 0, 0, 0)
		if err != nil {
			t.Fatalf("leveldb.New(%s, 0, 0) errored with %s", folder, err)
		}
		defer db.Close()

		test(t, db)
	}
}
