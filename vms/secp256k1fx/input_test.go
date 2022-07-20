// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInputVerifyNil(t *testing.T) {
	assert := assert.New(t)
	in := (*Input)(nil)
	assert.ErrorIs(in.Verify(), errNilInput)
}
