// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"testing"

	"github.com/stretchr/testify/require"

	dto "github.com/prometheus/client_model/go"
)

func TestMultiGathererEmptyGather(t *testing.T) {
	require := require.New(t)

	g := NewMultiGatherer()

	mfs, err := g.Gather()
	require.NoError(err)
	require.Empty(mfs)
}

func TestMultiGathererDuplicatedPrefix(t *testing.T) {
	require := require.New(t)

	g := NewMultiGatherer()
	og := NewOptionalGatherer()

	require.NoError(g.Register("", og))

	err := g.Register("", og)
	require.ErrorIs(err, errReregisterGatherer)

	require.NoError(g.Register("lol", og))
}

func TestMultiGathererAddedError(t *testing.T) {
	require := require.New(t)

	g := NewMultiGatherer()

	tg := &testGatherer{
		err: errTest,
	}

	require.NoError(g.Register("", tg))

	mfs, err := g.Gather()
	require.ErrorIs(err, errTest)
	require.Empty(mfs)
}

func TestMultiGathererNoAddedPrefix(t *testing.T) {
	require := require.New(t)

	g := NewMultiGatherer()

	tg := &testGatherer{
		mfs: []*dto.MetricFamily{{
			Name: &hello,
		}},
	}

	require.NoError(g.Register("", tg))

	mfs, err := g.Gather()
	require.NoError(err)
	require.Len(mfs, 1)
	require.Equal(&hello, mfs[0].Name)
}

func TestMultiGathererAddedPrefix(t *testing.T) {
	require := require.New(t)

	g := NewMultiGatherer()

	tg := &testGatherer{
		mfs: []*dto.MetricFamily{{
			Name: &world,
		}},
	}

	require.NoError(g.Register(hello, tg))

	mfs, err := g.Gather()
	require.NoError(err)
	require.Len(mfs, 1)
	require.Equal(&helloWorld, mfs[0].Name)
}

func TestMultiGathererJustPrefix(t *testing.T) {
	require := require.New(t)

	g := NewMultiGatherer()

	tg := &testGatherer{
		mfs: []*dto.MetricFamily{{}},
	}

	require.NoError(g.Register(hello, tg))

	mfs, err := g.Gather()
	require.NoError(err)
	require.Len(mfs, 1)
	require.Equal(&hello, mfs[0].Name)
}

func TestMultiGathererSorted(t *testing.T) {
	require := require.New(t)

	g := NewMultiGatherer()

	name0 := "a"
	name1 := "z"
	tg := &testGatherer{
		mfs: []*dto.MetricFamily{
			{
				Name: &name1,
			},
			{
				Name: &name0,
			},
		},
	}

	require.NoError(g.Register("", tg))

	mfs, err := g.Gather()
	require.NoError(err)
	require.Len(mfs, 2)
	require.Equal(&name0, mfs[0].Name)
	require.Equal(&name1, mfs[1].Name)
}
