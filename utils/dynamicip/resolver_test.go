// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamicip

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewResolver(t *testing.T) {
	type test struct {
		service      ResolverName
		validService bool
	}
	tests := []test{
		{
			service:      OpenDNS,
			validService: true,
		},
		{
			service:      IFConfig,
			validService: true,
		},
		{
			service:      IFConfigCo,
			validService: true,
		},
		{
			service:      IFConfigMe,
			validService: true,
		},
		{
			service:      ResolverName("not a valid resolver"),
			validService: false,
		},
	}
	for _, tt := range tests {
		t.Run(string(tt.service), func(t *testing.T) {
			assert := assert.New(t)
			_, err := NewResolver(tt.service)
			if tt.validService {
				assert.NoError(err)
			} else {
				assert.Error(err)
			}
		})
	}
}
