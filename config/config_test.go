// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/ids"
)

func setupViper() *viper.Viper {
	v := viper.New()
	fs := avalancheFlagSet()
	pflag.CommandLine.AddGoFlagSet(fs)
	pflag.Parse()
	err := v.BindPFlags(pflag.CommandLine)
	if err != nil {
		log.Fatal(err)
	}
	// need to set it since Working dir is wrong
	currentPath, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	v.Set(BuildDirKey, filepath.Join(currentPath, "..", "build"))
	return v
}

func TestChainConfigs(t *testing.T) {
	tests := map[string]struct {
		configs  map[string]string
		upgrades map[string]string
		expected map[string]chains.ChainConfig
		err      string
	}{
		"no chain configs": {
			configs:  map[string]string{},
			upgrades: map[string]string{},
			expected: map[string]chains.ChainConfig{},
		},
		"valid chain-id": {
			configs:  map[string]string{"yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp": "hello", "2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm": "world"},
			upgrades: map[string]string{"yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp": "helloUpgrades"},
			expected: func() map[string]chains.ChainConfig {
				m := map[string]chains.ChainConfig{}
				id1, err := ids.FromString("yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp")
				assert.NoError(t, err)
				m[id1.String()] = chains.ChainConfig{Config: []byte("hello"), Upgrade: []byte("helloUpgrades")}

				id2, err := ids.FromString("2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm")
				assert.NoError(t, err)
				m[id2.String()] = chains.ChainConfig{Config: []byte("world"), Upgrade: []byte(nil)}

				return m
			}(),
		},
		"valid alias": {
			configs:  map[string]string{"C": "hello", "X": "world"},
			upgrades: map[string]string{"C": "upgradess"},
			expected: func() map[string]chains.ChainConfig {
				m := map[string]chains.ChainConfig{}
				m["C"] = chains.ChainConfig{Config: []byte("hello"), Upgrade: []byte("upgradess")}
				m["X"] = chains.ChainConfig{Config: []byte("world"), Upgrade: []byte(nil)}

				return m
			}(),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			root := t.TempDir()
			configFile := path.Join(root, "config.json")
			configJSON := fmt.Sprintf(`{"chainconfig-dir": %q}`, root)

			assert.NoError(ioutil.WriteFile(configFile, []byte(configJSON), 0600))
			configsDir := path.Join(root, "chains", "configs")
			upgradesDir := path.Join(root, "chains", "upgrades")
			assert.NoError(os.MkdirAll(configsDir, 0700))
			assert.NoError(os.MkdirAll(upgradesDir, 0700))

			// Create custom configs
			for key, value := range test.configs {
				cConfigFile := path.Join(configsDir, key+".ex")
				assert.NoError(ioutil.WriteFile(cConfigFile, []byte(value), 0600))
			}
			for key, value := range test.upgrades {
				cUpgradeFile := path.Join(upgradesDir, key+".ex")
				assert.NoError(ioutil.WriteFile(cUpgradeFile, []byte(value), 0600))
			}
			v := setupViper()
			v.SetConfigFile(configFile)
			assert.NoError(v.ReadInConfig())

			// Parse config
			assert.Equal(root, v.GetString(ChainConfigDirKey))
			nodeConfig, _, err := getConfigsFromViper(v)
			if len(test.err) > 0 {
				assert.Error(err)
				assert.Contains(err.Error(), test.err)
				return
			}
			assert.NoError(err)
			assert.Equal(test.expected, nodeConfig.ChainConfigs)
		})
	}
}

func TestChainDirStructure(t *testing.T) {
	tests := map[string]struct {
		structure string
		file      map[string]string
		err       string
		expected  map[string]chains.ChainConfig
	}{
		"root dir not exist": {
			structure: "",
			file:      map[string]string{"C": "noeffect"},
			expected:  map[string]chains.ChainConfig{},
			err:       "",
		},
		"chains dir not exist": {
			structure: "cdir/",
			file:      map[string]string{"C": "noeffect"},
			expected:  map[string]chains.ChainConfig{},
			err:       "",
		},
		"configs dir not exist": {
			structure: "cdir/chains/",
			file:      map[string]string{"C": "noeffect"},
			expected:  map[string]chains.ChainConfig{},
			err:       "",
		},
		"full config structure": {
			structure: "cdir/chains/configs",
			file:      map[string]string{"C": "helloworld"},
			expected:  map[string]chains.ChainConfig{"C": {Config: []byte("helloworld")}},
			err:       "",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			root := t.TempDir()
			chainConfigDir := path.Join(root, "cdir")
			configFile := path.Join(root, "config.json")
			configJSON := fmt.Sprintf(`{"chainconfig-dir": %q}`, chainConfigDir)
			assert.NoError(ioutil.WriteFile(configFile, []byte(configJSON), 0600))
			dirToCreate := path.Join(root, test.structure)
			assert.NoError(os.MkdirAll(dirToCreate, 0700))
			for key, value := range test.file {
				file := path.Join(dirToCreate, key+".ex")
				assert.NoError(ioutil.WriteFile(file, []byte(value), 0600))
			}
			v := setupViper()
			v.SetConfigFile(configFile)
			assert.NoError(v.ReadInConfig())

			// Parse config
			assert.Equal(chainConfigDir, v.GetString(ChainConfigDirKey))
			nodeConfig, _, err := getConfigsFromViper(v)
			if len(test.err) > 0 {
				assert.Error(err)
				assert.Contains(err.Error(), test.err)
				return
			}
			assert.Equal(test.expected, nodeConfig.ChainConfigs)
		})
	}
}
