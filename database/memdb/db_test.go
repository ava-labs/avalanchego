// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package memdb

import (
	"testing"

	"github.com/ava-labs/avalanchego/database"
)

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		test(t, New())
	}
}
