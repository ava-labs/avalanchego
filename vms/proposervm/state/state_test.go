// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestState(t *testing.T) {
	a := assert.New(t)

	db := memdb.New()
	s := New(db)

	testBlockState(a, s)
	testChainState(a, s)
}

func TestMeteredState(t *testing.T) {
	a := assert.New(t)

	db := memdb.New()
	s, err := NewMetered(db, "", prometheus.NewRegistry())
	a.NoError(err)

	testBlockState(a, s)
	testChainState(a, s)
}
