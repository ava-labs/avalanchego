// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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

	err := g.Register(og)
	require.NoError(err)

	err = g.Register(og)
	require.Equal(errDuplicatedRegister, err)
}

func TestOptionalGathererAddedError(t *testing.T) {
	require := require.New(t)

	g := NewOptionalGatherer()

	tg := &testGatherer{
		err: errTest,
	}

	err := g.Register(tg)
	require.NoError(err)

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

	err := g.Register(tg)
	require.NoError(err)

	mfs, err := g.Gather()
	require.NoError(err)
	require.Len(mfs, 1)
	require.Equal(&hello, mfs[0].Name)
}
