// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestValidatorDataCompare(t *testing.T) {
	tests := []struct {
		a        validatorData
		b        validatorData
		expected int
	}{
		{
			a:        validatorData{},
			b:        validatorData{},
			expected: 0,
		},
		{
			a: validatorData{
				id: ids.BuildTestNodeID([]byte{1}),
			},
			b:        validatorData{},
			expected: 1,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s_%s_%d", test.a.id, test.b.id, test.expected), func(t *testing.T) {
			require := require.New(t)

			require.Equal(test.expected, test.a.Compare(test.b))
			require.Equal(-test.expected, test.b.Compare(test.a))
		})
	}
}
