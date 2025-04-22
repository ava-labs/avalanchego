// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package context

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type testConfig struct {
	TxFee  uint64 `json:"txFee"`
	MinFee uint64 `json:"minFee"`
}

func TestGetConfig(t *testing.T) {
	type test struct {
		name          string
		providedStr   string
		defaultConfig testConfig
		wantConfig    testConfig
	}
	for _, test := range []test{
		{
			name:          "default want non-zero values",
			providedStr:   "",
			defaultConfig: testConfig{TxFee: 100},
			wantConfig:    testConfig{TxFee: 100},
		},
		{
			name:          "default want zero values",
			providedStr:   "",
			defaultConfig: testConfig{},
			wantConfig:    testConfig{},
		},
		{
			name: "override default with zero values",
			providedStr: `{
				"test": {
					"txFee": 0,
					"minFee": 0
				}
			}`,
			defaultConfig: testConfig{TxFee: 100, MinFee: 100},
			wantConfig:    testConfig{TxFee: 0, MinFee: 0},
		},
		{
			name: "override non-zero defaults",
			providedStr: `{
				"test": {
					"txFee": 1000,
					"minFee": 1000
				}
			}`,
			defaultConfig: testConfig{TxFee: 100, MinFee: 100},
			wantConfig:    testConfig{TxFee: 1000, MinFee: 1000},
		},
		{
			name: "override one default value",
			providedStr: `{
				"test": {
					"txFee": 1000
				}
			}`,
			defaultConfig: testConfig{TxFee: 100, MinFee: 100},
			wantConfig:    testConfig{TxFee: 1000, MinFee: 100},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)
			c, err := NewConfig([]byte(test.providedStr))
			r.NoError(err)
			testConfig, err := GetConfig(c, "test", test.defaultConfig)
			r.NoError(err)
			r.Equal(test.wantConfig, testConfig)
		})
	}
}

func TestInvalidConfig(t *testing.T) {
	r := require.New(t)
	_, err := NewConfig([]byte(`{`))
	r.Error(err)
}

func TestInvalidConfigField(t *testing.T) {
	r := require.New(t)
	c, err := NewConfig([]byte(`{"test": {"txFee": "invalid"}}`))
	r.NoError(err)
	_, err = GetConfig(c, "test", testConfig{})
	r.Error(err)
}
