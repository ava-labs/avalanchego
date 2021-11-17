// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddressConversion(t *testing.T) {
	assert := assert.New(t)

	tests := []struct {
		chain         string
		hrp           string
		srcAddrs      []string
		expectedErr   error
		expectedAddrs []string
	}{
		{
			chain: "X",
			hrp:   "custom",
			srcAddrs: []string{
				"X-local1g65uqn6t77p656w64023nh8nd9updzmxyymev2",
				"X-local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u",
			},
			expectedAddrs: []string{
				"X-custom1g65uqn6t77p656w64023nh8nd9updzmxwd59gh",
				"X-custom18jma8ppw3nhx5r4ap8clazz0dps7rv5u9xde7p",
			},
		},
		{
			chain: "X",
			hrp:   "local",
			srcAddrs: []string{
				"X-local1g65uqn6t77p656w64023nh8nd9updzmxyymev2",
				"X-local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u",
			},
			expectedAddrs: []string{
				"X-local1g65uqn6t77p656w64023nh8nd9updzmxyymev2",
				"X-local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u",
			},
		},
		{
			srcAddrs: []string{
				"X-local1g65uqn6t77p656w64023nh8nd9updzmxyymev2",
			},
			expectedAddrs: []string{
				"-1g65uqn6t77p656w64023nh8nd9updzmx4x372p",
			},
		},
		{
			srcAddrs: []string{
				"not a valid address",
			},
			expectedErr: errNoSeparator,
		},
	}
	for _, test := range tests {
		result, err := ConvertAddresses(test.chain, test.hrp, test.srcAddrs)
		assert.Equal(test.expectedErr, err)
		assert.Len(result, len(test.expectedAddrs))
		for i, want := range test.expectedAddrs {
			got := result[i]
			assert.Equal(want, got)
		}
	}
}
