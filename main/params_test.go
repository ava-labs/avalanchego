package main

import (
	"io/ioutil"
	"path"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/snow"
)

func TestChainConfigs(t *testing.T) {
	tests := map[string]struct {
		config string

		expected map[ids.ID]snow.ChainConfig
		err      string
	}{
		"no chain configs": {
			config:   `{}`,
			expected: map[ids.ID]snow.ChainConfig{},
		},
		"valid chain-id (default settings)": {
			config: `{
				"chain-configs": [
					{"chain-id": "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp", "settings": "default", "forks": "default"},
					{"chain-id": "2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm", "settings": "blah1", "forks": "blah2"}
				]
			}`,
			expected: func() map[ids.ID]snow.ChainConfig {
				m := map[ids.ID]snow.ChainConfig{}
				id1, err := ids.FromString("yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp")
				assert.NoError(t, err)
				m[id1] = snow.ChainConfig{Settings: "default", Forks: "default"}

				id2, err := ids.FromString("2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm")
				assert.NoError(t, err)
				m[id2] = snow.ChainConfig{Settings: "blah1", Forks: "blah2"}

				return m
			}(),
		},
		"valid chain-id (JSON settings)": {
			config: `{
				"chain-configs": [
					{"chain-id": "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp", "settings": {
						"snowman-api-enabled": true
					}, "forks": "default"}
				]
			}`,
			expected: func() map[ids.ID]snow.ChainConfig {
				m := map[ids.ID]snow.ChainConfig{}
				id1, err := ids.FromString("yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp")
				assert.NoError(t, err)
				m[id1] = snow.ChainConfig{Settings: `{"snowman-api-enabled":true}`, Forks: "default"}

				return m
			}(),
		},
		"invalid checksum chain-id": {
			config: `{
				"chain-configs": [
					{"chain-id": "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWq", "settings": "default", "forks": "default"}
				]
			}`,
			err: "couldn't parse chainID yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWq: invalid input checksum",
		},
		"missing chain-id": {
			config: `{
				"chain-configs": [
					{"settings": "default", "forks": "default"}
				]
			}`,
			err: "couldn't parse ChainID from chain config 0",
		},
		"non-string chain-id": {
			config: `{
				"chain-configs": [
					{"chain-id": 1, "settings": "default", "forks": "default"}
				]
			}`,
			err: "ChainID `1` is not a string in chain config 0",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			// Create custom config
			config := path.Join(t.TempDir(), "config.json")
			assert.NoError(ioutil.WriteFile(config, []byte(test.config), 0600))

			// Setup Viper
			v := viper.New()
			fs := avalancheFlagSet()
			pflag.CommandLine.AddGoFlagSet(fs)
			pflag.Parse()
			assert.NoError(v.BindPFlags(pflag.CommandLine))
			v.SetConfigFile(config)
			assert.NoError(v.ReadInConfig())

			// Parse config
			v.Set(logsDirKey, t.TempDir())
			v.Set(dbDirKey, t.TempDir())
			Config = node.Config{}
			err := setNodeConfig(v)
			if len(test.err) > 0 {
				assert.Error(err)
				assert.Contains(err.Error(), test.err)
				return
			}
			assert.Equal(test.expected, Config.ChainConfigs)
		})
	}
}
