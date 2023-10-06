// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
)

func TestLess(t *testing.T) {
	tests := []struct {
		name     string
		a        avalanche.Vertex
		b        avalanche.Vertex
		expected bool
	}{
		{
			name: "a less than b - a unknown b accepted",
			a:    &avalanche.TestVertex{},
			b: &avalanche.TestVertex{
				TestDecidable: choices.TestDecidable{
					StatusV: choices.Accepted,
				},
			},
			expected: true,
		},
		{
			name: "a not less than b - a accepted b unknown",
			a: &avalanche.TestVertex{
				TestDecidable: choices.TestDecidable{
					StatusV: choices.Accepted,
				},
			},
			b:        &avalanche.TestVertex{},
			expected: false,
		},
		{
			name: "a less than b - ties broken by height",
			a: &avalanche.TestVertex{
				TestDecidable: choices.TestDecidable{
					StatusV: choices.Accepted,
				},
				HeightV: 1,
			},
			b: &avalanche.TestVertex{
				TestDecidable: choices.TestDecidable{
					StatusV: choices.Accepted,
				},
				HeightV: 0,
			},
			expected: true,
		},
		{
			name: "a not less than b - ties broken by height",
			a: &avalanche.TestVertex{
				TestDecidable: choices.TestDecidable{
					StatusV: choices.Accepted,
				},
				HeightV: 0,
			},
			b: &avalanche.TestVertex{
				TestDecidable: choices.TestDecidable{
					StatusV: choices.Accepted,
				},
				HeightV: 1,
			},
			expected: false,
		},
		{
			name: "a not less than b - equality",
			a: &avalanche.TestVertex{
				TestDecidable: choices.TestDecidable{
					StatusV: choices.Accepted,
				},
				HeightV: 1,
			},
			b: &avalanche.TestVertex{
				TestDecidable: choices.TestDecidable{
					StatusV: choices.Accepted,
				},
				HeightV: 1,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, Less(tt.a, tt.b))
		})
	}
}
