// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !prod

package gastime

import (
	"github.com/ava-labs/libevm/libevm/testonly"

	"github.com/ava-labs/avalanchego/vms/components/gas"
)

// TestOnlySetExcess permanently overrides the [Time.Excess] but panics if
// called outside of a testing environment. See [testonly.OrPanic].
func (tm *Time) TestOnlySetExcess(x gas.Gas) {
	testonly.OrPanic(func() {
		tm.excess = x
	})
}
