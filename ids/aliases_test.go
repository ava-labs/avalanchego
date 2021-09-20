// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"testing"
)

func TestAliaser(t *testing.T) {
	for _, test := range AliasTests {
		aliaser := NewAliaser()
		test(t, aliaser, aliaser)
	}
}
