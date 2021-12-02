// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	dto "github.com/prometheus/client_model/go"
)

func TestOptionalGathererEmptyGather(t *testing.T) {
	assert := assert.New(t)

	g := NewOptionalGatherer()

	mfs, err := g.Gather()
	assert.NoError(err)
	assert.Empty(mfs)
}

func TestOptionalGathererDuplicated(t *testing.T) {
	assert := assert.New(t)

	g := NewOptionalGatherer()
	og := NewOptionalGatherer()

	err := g.Register(og)
	assert.NoError(err)

	err = g.Register(og)
	assert.Equal(errDuplicatedRegister, err)
}

func TestOptionalGathererAddedError(t *testing.T) {
	assert := assert.New(t)

	g := NewOptionalGatherer()

	expected := errors.New(":(")
	tg := &testGatherer{
		err: expected,
	}

	err := g.Register(tg)
	assert.NoError(err)

	mfs, err := g.Gather()
	assert.Equal(expected, err)
	assert.Empty(mfs)
}

func TestMultiGathererAdded(t *testing.T) {
	assert := assert.New(t)

	g := NewOptionalGatherer()

	tg := &testGatherer{
		mfs: []*dto.MetricFamily{{
			Name: &hello,
		}},
	}

	err := g.Register(tg)
	assert.NoError(err)

	mfs, err := g.Gather()
	assert.NoError(err)
	assert.Len(mfs, 1)
	assert.Equal(&hello, mfs[0].Name)
}
