// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	dto "github.com/prometheus/client_model/go"
)

func TestMultiGathererEmptyGather(t *testing.T) {
	assert := assert.New(t)

	g := NewMultiGatherer()

	mfs, err := g.Gather()
	assert.NoError(err)
	assert.Empty(mfs)
}

func TestMultiGathererDuplicatedPrefix(t *testing.T) {
	assert := assert.New(t)

	g := NewMultiGatherer()
	og := NewOptionalGatherer()

	err := g.Register("", og)
	assert.NoError(err)

	err = g.Register("", og)
	assert.Equal(errDuplicatedPrefix, err)

	err = g.Register("lol", og)
	assert.NoError(err)
}

func TestMultiGathererAddedError(t *testing.T) {
	assert := assert.New(t)

	g := NewMultiGatherer()

	expected := errors.New(":(")
	tg := &testGatherer{
		err: expected,
	}

	err := g.Register("", tg)
	assert.NoError(err)

	mfs, err := g.Gather()
	assert.Equal(expected, err)
	assert.Empty(mfs)
}

func TestMultiGathererNoAddedPrefix(t *testing.T) {
	assert := assert.New(t)

	g := NewMultiGatherer()

	tg := &testGatherer{
		mfs: []*dto.MetricFamily{{
			Name: &hello,
		}},
	}

	err := g.Register("", tg)
	assert.NoError(err)

	mfs, err := g.Gather()
	assert.NoError(err)
	assert.Len(mfs, 1)
	assert.Equal(&hello, mfs[0].Name)
}

func TestMultiGathererAddedPrefix(t *testing.T) {
	assert := assert.New(t)

	g := NewMultiGatherer()

	tg := &testGatherer{
		mfs: []*dto.MetricFamily{{
			Name: &world,
		}},
	}

	err := g.Register(hello, tg)
	assert.NoError(err)

	mfs, err := g.Gather()
	assert.NoError(err)
	assert.Len(mfs, 1)
	assert.Equal(&helloWorld, mfs[0].Name)
}

func TestMultiGathererJustPrefix(t *testing.T) {
	assert := assert.New(t)

	g := NewMultiGatherer()

	tg := &testGatherer{
		mfs: []*dto.MetricFamily{{}},
	}

	err := g.Register(hello, tg)
	assert.NoError(err)

	mfs, err := g.Gather()
	assert.NoError(err)
	assert.Len(mfs, 1)
	assert.Equal(&hello, mfs[0].Name)
}

func TestMultiGathererSorted(t *testing.T) {
	assert := assert.New(t)

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
	assert.NoError(err)

	mfs, err := g.Gather()
	assert.NoError(err)
	assert.Len(mfs, 2)
	assert.Equal(&name0, mfs[0].Name)
	assert.Equal(&name1, mfs[1].Name)
}
