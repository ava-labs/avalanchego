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

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils/logging"

	"github.com/stretchr/testify/assert"
)

type CounterHandler struct {
	Tx int
}

func (h *CounterHandler) HandleTx(ids.ShortID, uint32, *Tx) error {
	h.Tx++
	return nil
}

func TestHandleTx(t *testing.T) {
	assert := assert.New(t)

	handler := CounterHandler{}
	msg := Tx{}

	err := msg.Handle(&handler, ids.ShortEmpty, 0)
	assert.NoError(err)
	assert.Equal(1, handler.Tx)
}

func TestNoopHandler(t *testing.T) {
	assert := assert.New(t)

	handler := NoopHandler{
		Log: logging.NoLog{},
	}

	err := handler.HandleTx(ids.ShortEmpty, 0, nil)
	assert.NoError(err)
}
