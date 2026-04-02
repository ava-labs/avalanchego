// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linkeddb

import (
	"testing"

	"github.com/ava-labs/avalanchego/database/dbtest"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestInterface(t *testing.T) {
	for name, test := range dbtest.TestsBasic {
		t.Run(name, func(t *testing.T) {
			db := NewDefault(memdb.New())
			test(t, db)
		})
	}
}

func FuzzKeyValue(f *testing.F) {
	db := NewDefault(memdb.New())
	dbtest.FuzzKeyValue(f, db)
}
