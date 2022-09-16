// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	dto "github.com/prometheus/client_model/go"
)

func TestOptionalGathererEmptyGather(t *testing.T) {
	require := require.New(t)

	g := NewOptionalGatherer()

	mfs, err := g.Gather()
	require.NoError(err)
	require.Empty(mfs)
}

func TestOptionalGathererDuplicated(t *testing.T) {
	require := require.New(t)

	g := NewOptionalGatherer()
	og := NewOptionalGatherer()

	err := g.Register(og)
	require.NoError(err)

	err = g.Register(og)
	require.Equal(errDuplicatedRegister, err)
}

func TestOptionalGathererAddedError(t *testing.T) {
	require := require.New(t)

	g := NewOptionalGatherer()

	expected := errors.New(":(")
	tg := &testGatherer{
		err: expected,
	}

	err := g.Register(tg)
	require.NoError(err)

	mfs, err := g.Gather()
	require.Equal(expected, err)
	require.Empty(mfs)
}

func TestMultiGathererAdded(t *testing.T) {
	require := require.New(t)

	g := NewOptionalGatherer()

	tg := &testGatherer{
		mfs: []*dto.MetricFamily{{
			Name: &hello,
		}},
	}

	err := g.Register(tg)
	require.NoError(err)

	mfs, err := g.Gather()
	require.NoError(err)
	require.Len(mfs, 1)
	require.Equal(&hello, mfs[0].Name)
}
