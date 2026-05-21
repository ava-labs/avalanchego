// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package memdb

import (
	"testing"

	"github.com/MetalBlockchain/metalgo/database"
	"github.com/MetalBlockchain/metalgo/database/heightindexdb/dbtest"
)

func TestInterface(t *testing.T) {
	for _, test := range dbtest.Tests {
		t.Run("memdb_"+test.Name, func(t *testing.T) {
			test.Test(t, func() database.HeightIndex { return &Database{} })
		})
	}
}
