// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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

	err := g.Register("", og)
	require.NoError(err)

	err = g.Register("", og)
	require.Equal(errDuplicatedPrefix, err)

	err = g.Register("lol", og)
	require.NoError(err)
}

func TestMultiGathererAddedError(t *testing.T) {
	require := require.New(t)

	g := NewMultiGatherer()

	tg := &testGatherer{
		err: errTest,
	}

	err := g.Register("", tg)
	require.NoError(err)

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

	err := g.Register("", tg)
	require.NoError(err)

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

	err := g.Register(hello, tg)
	require.NoError(err)

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

	err := g.Register(hello, tg)
	require.NoError(err)

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

	err := g.Register("", tg)
	require.NoError(err)

	mfs, err := g.Gather()
	require.NoError(err)
	require.Len(mfs, 2)
	require.Equal(&name0, mfs[0].Name)
	require.Equal(&name1, mfs[1].Name)
}
