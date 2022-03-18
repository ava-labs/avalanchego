// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ulimit

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// Test_SetDefault performs sanity checks for the os default.
func Test_SetDefault(t *testing.T) {
	assert := assert.New(t)
	err := Set(DefaultFDLimit, logging.NoLog{})
	assert.NoErrorf(err, "default fd-limit failed %v", err)
}
