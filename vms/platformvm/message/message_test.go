// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/chain4travel/caminogo/utils"
	"github.com/chain4travel/caminogo/utils/units"

	"github.com/stretchr/testify/assert"
)

func TestTx(t *testing.T) {
	assert := assert.New(t)

	tx := utils.RandomBytes(256 * units.KiB)
	builtMsg := Tx{
		Tx: tx,
	}
	builtMsgBytes, err := Build(&builtMsg)
	assert.NoError(err)
	assert.Equal(builtMsgBytes, builtMsg.Bytes())

	parsedMsgIntf, err := Parse(builtMsgBytes)
	assert.NoError(err)
	assert.Equal(builtMsgBytes, parsedMsgIntf.Bytes())

	parsedMsg, ok := parsedMsgIntf.(*Tx)
	assert.True(ok)

	assert.Equal(tx, parsedMsg.Tx)
}

func TestParseGibberish(t *testing.T) {
	assert := assert.New(t)

	randomBytes := utils.RandomBytes(256 * units.KiB)
	_, err := Parse(randomBytes)
	assert.Error(err)
}
