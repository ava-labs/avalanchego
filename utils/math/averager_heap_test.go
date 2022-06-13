// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func TestAveragerHeap(t *testing.T) {
	assert := assert.New(t)

	n0 := ids.GenerateTestNodeID()
	n1 := ids.GenerateTestNodeID()
	n2 := ids.GenerateTestNodeID()

	tests := []struct {
		h AveragerHeap
		a []Averager
	}{
		{
			h: NewMinAveragerHeap(),
			a: []Averager{
				NewAverager(0, time.Second, time.Now()),
				NewAverager(1, time.Second, time.Now()),
				NewAverager(2, time.Second, time.Now()),
			},
		},
		{
			h: NewMaxAveragerHeap(),
			a: []Averager{
				NewAverager(0, time.Second, time.Now()),
				NewAverager(-1, time.Second, time.Now()),
				NewAverager(-2, time.Second, time.Now()),
			},
		},
	}

	for _, test := range tests {
		_, _, ok := test.h.Pop()
		assert.False(ok)

		_, _, ok = test.h.Peek()
		assert.False(ok)

		l := test.h.Len()
		assert.Zero(l)

		_, ok = test.h.Add(n1, test.a[1])
		assert.False(ok)

		n, a, ok := test.h.Peek()
		assert.True(ok)
		assert.Equal(n1, n)
		assert.Equal(test.a[1], a)

		l = test.h.Len()
		assert.Equal(1, l)

		a, ok = test.h.Add(n1, test.a[1])
		assert.True(ok)
		assert.Equal(test.a[1], a)

		l = test.h.Len()
		assert.Equal(1, l)

		_, ok = test.h.Add(n0, test.a[0])
		assert.False(ok)

		_, ok = test.h.Add(n2, test.a[2])
		assert.False(ok)

		n, a, ok = test.h.Pop()
		assert.True(ok)
		assert.Equal(n0, n)
		assert.Equal(test.a[0], a)

		l = test.h.Len()
		assert.Equal(2, l)

		a, ok = test.h.Remove(n1)
		assert.True(ok)
		assert.Equal(test.a[1], a)

		l = test.h.Len()
		assert.Equal(1, l)

		_, ok = test.h.Remove(n1)
		assert.False(ok)

		l = test.h.Len()
		assert.Equal(1, l)

		a, ok = test.h.Add(n2, test.a[0])
		assert.True(ok)
		assert.Equal(test.a[2], a)

		n, a, ok = test.h.Pop()
		assert.True(ok)
		assert.Equal(n2, n)
		assert.Equal(test.a[0], a)
	}
}
