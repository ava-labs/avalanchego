// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnmarshalBaseConfig(t *testing.T) {
	tests := CommonUnmarshalTests(func(c BaseConfig) BaseConfig {
		return c
	})
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			var got BaseConfig
			err := json.Unmarshal(tt.GivenJSON, &got)
			require.NoError(t, err)
			require.Equal(t, tt.Want, got)
		})
	}
}
