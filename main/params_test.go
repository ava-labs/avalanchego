package main

import (
	"io/ioutil"
	"path"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func TestChainConfigs(t *testing.T) {
	tests := map[string]struct {
		config string

		expected map[ids.ID]chains.ChainConfig
		err      string
	}{
		"no chain configs": {
			config:   `{}`,
			expected: map[ids.ID]chains.ChainConfig{},
		},
		"valid chain-id (default settings)": {
			config: `{
				"chain-configs": [
					{"chain-id": "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp", "settings": "default", "upgrades": "default"},
					{"chain-id": "2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm", "settings": "blah1", "upgrades": "blah2"}
				]
			}`,
			expected: func() map[ids.ID]chains.ChainConfig {
				m := map[ids.ID]chains.ChainConfig{}
				id1, err := ids.FromString("yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp")
				assert.NoError(t, err)
				m[id1] = chains.ChainConfig{Settings: []byte("default"), Upgrades: []byte("default")}

				id2, err := ids.FromString("2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm")
				assert.NoError(t, err)
				m[id2] = chains.ChainConfig{Settings: []byte("blah1"), Upgrades: []byte("blah2")}

				return m
			}(),
		},
		"valid chain-id (JSON settings)": {
			config: `{
				"chain-configs": [
					{"chain-id": "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp", "settings": {
						"snowman-api-enabled": true
					}, "upgrades": "default"}
				]
			}`,
			expected: func() map[ids.ID]chains.ChainConfig {
				m := map[ids.ID]chains.ChainConfig{}
				id1, err := ids.FromString("yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp")
				assert.NoError(t, err)
				m[id1] = chains.ChainConfig{Settings: []byte(`{"snowman-api-enabled":true}`), Upgrades: []byte("default")}

				return m
			}(),
		},
		"invalid structure (array of strings)": {
			config: `{
				"chain-configs": [
					"hello"
				]
			}`,
			err: `'[0]' expected a map, got 'string'`,
		},
		"invalid checksum chain-id": {
			config: `{
				"chain-configs": [
					{"chain-id": "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWq", "settings": "default", "upgrades": "default"}
				]
			}`,
			err: "could not parse chainID yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWq: invalid input checksum",
		},
		"missing chain-id": {
			config: `{
				"chain-configs": [
					{"settings": "default", "upgrades": "default"}
				]
			}`,
			err: "could not parse chainID from chain config 0",
		},
		"non-string chain-id": {
			config: `{
				"chain-configs": [
					{"chain-id": 1, "settings": "default", "upgrades": "default"}
				]
			}`,
			err: "chainID `1` is not a string in chain config 0",
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
			v.Set(networkNameKey, constants.LocalName)
			v.Set(dbEnabledKey, false)
			v.Set(publicIPKey, "1.1.1.1")
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
