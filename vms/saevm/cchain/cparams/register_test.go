// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cparams

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	ethparams "github.com/ava-labs/libevm/params"
)

func TestMain(m *testing.M) {
	RegisterExtras()
	os.Exit(m.Run())
}

func TestGetExtraNil(t *testing.T) {
	c := &ethparams.ChainConfig{}
	require.Nil(t, GetExtra(c), "GetExtra() before WithExtra()")
}

func TestGetExtra(t *testing.T) {
	c := &ethparams.ChainConfig{}
	extra := &ChainConfig{}
	require.Same(t, c, WithExtra(c, extra), "WithExtra() return value")
	require.Same(t, extra, GetExtra(c), "GetExtra() after WithExtra()")
}
