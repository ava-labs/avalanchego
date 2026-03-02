// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestAveragerHeap(t *testing.T) {
	n0 := ids.GenerateTestNodeID()
	n1 := ids.GenerateTestNodeID()
	n2 := ids.GenerateTestNodeID()

	tests := []struct {
		name string
		h    AveragerHeap
		a    []Averager
	}{
		{
			name: "max heap",
			h:    NewMaxAveragerHeap(),
			a: []Averager{
				NewAverager(0, time.Second, time.Now()),
				NewAverager(-1, time.Second, time.Now()),
				NewAverager(-2, time.Second, time.Now()),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			_, _, ok := test.h.Pop()
			require.False(ok)

			_, _, ok = test.h.Peek()
			require.False(ok)

			l := test.h.Len()
			require.Zero(l)

			_, ok = test.h.Add(n1, test.a[1])
			require.False(ok)

			n, a, ok := test.h.Peek()
			require.True(ok)
			require.Equal(n1, n)
			require.Equal(test.a[1], a)

			l = test.h.Len()
			require.Equal(1, l)

			a, ok = test.h.Add(n1, test.a[1])
			require.True(ok)
			require.Equal(test.a[1], a)

			l = test.h.Len()
			require.Equal(1, l)

			_, ok = test.h.Add(n0, test.a[0])
			require.False(ok)

			_, ok = test.h.Add(n2, test.a[2])
			require.False(ok)

			n, a, ok = test.h.Pop()
			require.True(ok)
			require.Equal(n0, n)
			require.Equal(test.a[0], a)

			l = test.h.Len()
			require.Equal(2, l)

			a, ok = test.h.Remove(n1)
			require.True(ok)
			require.Equal(test.a[1], a)

			l = test.h.Len()
			require.Equal(1, l)

			_, ok = test.h.Remove(n1)
			require.False(ok)

			l = test.h.Len()
			require.Equal(1, l)

			a, ok = test.h.Add(n2, test.a[0])
			require.True(ok)
			require.Equal(test.a[2], a)

			n, a, ok = test.h.Pop()
			require.True(ok)
			require.Equal(n2, n)
			require.Equal(test.a[0], a)
		})
	}
}
