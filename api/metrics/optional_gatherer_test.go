// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	dto "github.com/prometheus/client_model/go"
)

var errTest = errors.New("non-nil error")

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

	require.NoError(g.Register(og))
	err := g.Register(og)
	require.ErrorIs(err, errReregisterGatherer)
}

func TestOptionalGathererAddedError(t *testing.T) {
	require := require.New(t)

	g := NewOptionalGatherer()

	tg := &testGatherer{
		err: errTest,
	}

	require.NoError(g.Register(tg))

	mfs, err := g.Gather()
	require.ErrorIs(err, errTest)
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

	require.NoError(g.Register(tg))

	mfs, err := g.Gather()
	require.NoError(err)
	require.Len(mfs, 1)
	require.Equal(&hello, mfs[0].Name)
}
