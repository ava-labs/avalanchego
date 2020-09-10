// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prefixdb

import (
	"testing"

	"github.com/ava-labs/avalanche-go/database"
	"github.com/ava-labs/avalanche-go/database/memdb"
)

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		db := memdb.New()
		test(t, New([]byte("hello"), db))
		test(t, New([]byte("world"), db))
		test(t, New([]byte("wor"), New([]byte("ld"), db)))
		test(t, New([]byte("ld"), New([]byte("wor"), db)))
		test(t, NewNested([]byte("wor"), New([]byte("ld"), db)))
		test(t, NewNested([]byte("ld"), New([]byte("wor"), db)))
	}
}
