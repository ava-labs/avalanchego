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

func TestSetChainConfigs(t *testing.T) {
	tests := map[string]struct {
		configs      map[string]string
		upgrades     map[string]string
		corethConfig string
		errMessage   string
		expected     map[string]chains.ChainConfig
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
		"coreth config only": {
			configs:      map[string]string{},
			upgrades:     map[string]string{},
			corethConfig: "hello",
			expected:     map[string]chains.ChainConfig{"C": {Config: []byte("hello"), Upgrade: []byte(nil)}},
		},
		"coreth with c alias chain config": {
			configs:      map[string]string{"C": "hello", "X": "world"},
			upgrades:     map[string]string{"C": "upgradess"},
			corethConfig: "hellocoreth",
			errMessage:   "is already provided",
			expected:     nil,
		},
		"coreth with evm alias chain config": {
			configs:      map[string]string{"evm": "hello", "X": "world"},
			upgrades:     map[string]string{"evm": "upgradess"},
			corethConfig: "hellocoreth",
			errMessage:   "is already provided",
			expected:     nil,
		},
		"coreth and c chain upgrades in config": {
			configs:      map[string]string{"X": "world"},
			upgrades:     map[string]string{"C": "upgradess"},
			corethConfig: "hello",
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
			var configJSON string
			if len(test.corethConfig) > 0 {
				configJSON = fmt.Sprintf(`{%q: %q, %q: %q}`, ChainConfigDirKey, root, CorethConfigKey, test.corethConfig)
			} else {
				configJSON = fmt.Sprintf(`{%q: %q}`, ChainConfigDirKey, root)
			}
			configFile := setupConfigJSON(t, root, configJSON)
			chainsDir := root
			// Create custom configs
			for key, value := range test.configs {
				chainDir := path.Join(chainsDir, key)
				setupFile(t, chainDir, chainConfigFileName+".ex", value)
			}
			for key, value := range test.upgrades {
				chainDir := path.Join(chainsDir, key)
				setupFile(t, chainDir, chainUpgradeFileName+".ex", value)
			}

			v := setupViper(configFile)

			// Parse config
			assert.Equal(root, v.GetString(ChainConfigDirKey))
			chainConfigs, err := getChainConfigs(v)
			if len(test.errMessage) > 0 {
				assert.Error(err)
				if err != nil {
					assert.Contains(err.Error(), test.errMessage)
				}
			} else {
				assert.NoError(err)
			}
			assert.Equal(test.expected, chainConfigs)
		})
	}
}

func TestSetChainConfigsDirNotExist(t *testing.T) {
	tests := map[string]struct {
		structure  string
		file       map[string]string
		err        error
		errMessage string
		flagSet    bool
		expected   map[string]chains.ChainConfig
	}{
		"cdir not exist": {
			structure: "/",
			file:      map[string]string{"config.ex": "noeffect"},
			err:       os.ErrNotExist,
			flagSet:   true,
			expected:  nil,
		},
		"cdir is file ": {
			structure:  "/",
			file:       map[string]string{"cdir": "noeffect"},
			errMessage: "not a directory",
			flagSet:    true,
			expected:   nil,
		},
		"cdir not exist flag not set": {
			structure: "/",
			file:      map[string]string{"config.ex": "noeffect"},
			flagSet:   false,
			expected:  map[string]chains.ChainConfig{},
		},
		"chain subdir not exist": {
			structure: "/cdir/",
			file:      map[string]string{"config.ex": "noeffect"},
			flagSet:   true,
			expected:  map[string]chains.ChainConfig{},
		},
		"full structure": {
			structure: "/cdir/C/",
			file:      map[string]string{"config.ex": "hello"},
			flagSet:   true,
			expected:  map[string]chains.ChainConfig{"C": {Config: []byte("hello"), Upgrade: []byte(nil)}},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			root := t.TempDir()
			chainConfigDir := path.Join(root, "cdir")
			var configJSON string
			if test.flagSet {
				configJSON = fmt.Sprintf(`{%q: %q}`, ChainConfigDirKey, chainConfigDir)
			} else {
				configJSON = "{}"
			}
			configFile := setupConfigJSON(t, root, configJSON)

			dirToCreate := path.Join(root, test.structure)
			assert.NoError(os.MkdirAll(dirToCreate, 0700))

			for key, value := range test.file {
				setupFile(t, dirToCreate, key, value)
			}
			v := setupViper(configFile)

			// Parse config
			if test.flagSet {
				assert.Equal(chainConfigDir, v.GetString(ChainConfigDirKey))
			}
			// don't read with getConfigFromViper since it's very slow.
			chainConfigs, err := getChainConfigs(v)
			switch {
			case test.err != nil:
				assert.ErrorIs(err, test.err)
			case len(test.errMessage) > 0:
				assert.Error(err)
				assert.Contains(err.Error(), test.errMessage)
			default:
				assert.NoError(err)
				assert.Equal(test.expected, chainConfigs)
			}
		})
	}
}

func TestSetChainConfigDefaultDir(t *testing.T) {
	assert := assert.New(t)
	root := t.TempDir()
	// changes internal package variable, since using defaultDir (under user home) is risky.
	defaultChainConfigDir = path.Join(root, "cdir")
	configFilePath := setupConfigJSON(t, root, "{}")

	v := setupViper(configFilePath)
	assert.Equal(defaultChainConfigDir, v.GetString(ChainConfigDirKey))

	chainsDir := path.Join(defaultChainConfigDir, "C")
	setupFile(t, chainsDir, chainConfigFileName+".ex", "helloworld")
	chainConfigs, err := getChainConfigs(v)
	assert.NoError(err)
	expected := map[string]chains.ChainConfig{"C": {Config: []byte("helloworld"), Upgrade: []byte(nil)}}
	assert.Equal(expected, chainConfigs)
}

func TestReadVMAliases(t *testing.T) {
	tests := map[string]struct {
		givenJSON  string
		expected   map[ids.ID][]string
		errMessage string
	}{
		"wrong vm id": {
			givenJSON:  `{"wrongVmId": ["vm1","vm2"]}`,
			expected:   nil,
			errMessage: "problem unmarshaling vmAliases",
		},
		"vm id": {
			givenJSON: `{"2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i": ["vm1","vm2"],
										"Gmt4fuNsGJAd2PX86LBvycGaBpgCYKbuULdCLZs3SEs1Jx1LU": ["vm3", "vm4"] }`,
			expected: func() map[ids.ID][]string {
				m := map[ids.ID][]string{}
				id1, _ := ids.FromString("2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i")
				id2, _ := ids.FromString("Gmt4fuNsGJAd2PX86LBvycGaBpgCYKbuULdCLZs3SEs1Jx1LU")
				m[id1] = []string{"vm1", "vm2"}
				m[id2] = []string{"vm3", "vm4"}
				return m
			}(),
			errMessage: "",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			root := t.TempDir()
			aliasPath := path.Join(root, "aliases.json")
			configJSON := fmt.Sprintf(`{%q: %q}`, VMAliasesFileKey, aliasPath)
			configFilePath := setupConfigJSON(t, root, configJSON)
			setupFile(t, root, "aliases.json", test.givenJSON)
			v := setupViper(configFilePath)
			vmAliases, err := readVMAliases(v)
			if len(test.errMessage) > 0 {
				assert.Error(err)
				assert.Contains(err.Error(), test.errMessage)
			} else {
				assert.NoError(err)
				assert.Equal(test.expected, vmAliases)
			}
		})
	}
}

func TestReadVMAliasesDefaultDir(t *testing.T) {
	assert := assert.New(t)
	root := t.TempDir()
	// changes internal package variable, since using defaultDir (under user home) is risky.
	defaultVMAliasFilePath = filepath.Join(root, "aliases.json")
	configFilePath := setupConfigJSON(t, root, "{}")

	v := setupViper(configFilePath)
	assert.Equal(defaultVMAliasFilePath, v.GetString(VMAliasesFileKey))

	setupFile(t, root, "aliases.json", `{"2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i": ["vm1","vm2"]}`)
	vmAliases, err := readVMAliases(v)
	assert.NoError(err)

	expected := map[ids.ID][]string{}
	id, _ := ids.FromString("2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i")
	expected[id] = []string{"vm1", "vm2"}
	assert.Equal(expected, vmAliases)
}

func TestReadVMAliasesDirNotExists(t *testing.T) {
	assert := assert.New(t)
	root := t.TempDir()
	aliasPath := "/not/exists"
	// set it explicitly
	configJSON := fmt.Sprintf(`{%q: %q}`, VMAliasesFileKey, aliasPath)
	configFilePath := setupConfigJSON(t, root, configJSON)
	v := setupViper(configFilePath)
	vmAliases, err := readVMAliases(v)
	assert.Nil(vmAliases)
	assert.Error(err)
	assert.Contains(err.Error(), "vm alias file does not exist")

	// do not set it explicitly
	configJSON = "{}"
	configFilePath = setupConfigJSON(t, root, configJSON)
	v = setupViper(configFilePath)
	vmAliases, err = readVMAliases(v)
	assert.Nil(vmAliases)
	assert.NoError(err)
}

// setups config json file and writes content
func setupConfigJSON(t *testing.T, rootPath string, value string) string {
	configFilePath := path.Join(rootPath, "config.json")
	assert.NoError(t, ioutil.WriteFile(configFilePath, []byte(value), 0600))
	return configFilePath
}

// setups file creates necessary path and writes value to it.
func setupFile(t *testing.T, path string, fileName string, value string) {
	assert.NoError(t, os.MkdirAll(path, 0700))
	filePath := filepath.Join(path, fileName)
	assert.NoError(t, ioutil.WriteFile(filePath, []byte(value), 0600))
}

func setupViper(configFilePath string) *viper.Viper {
	v := viper.New()
	fs := BuildFlagSet()
	pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.PanicOnError) // flags are now reset
	pflag.CommandLine.AddGoFlagSet(fs)
	pflag.Parse()
	if err := v.BindPFlags(pflag.CommandLine); err != nil {
		log.Fatal(err)
	}
	// need to set it since in tests executable dir is somewhere /var/tmp/ (or wherever is designated by go)
	// thus it searches buildDir in /var/tmp/
	// but actual buildDir resides under project_root/build
	currentPath, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	v.Set(BuildDirKey, filepath.Join(currentPath, "..", "build"))
	v.SetConfigFile(configFilePath)
	err = v.ReadInConfig()
	if err != nil {
		log.Fatal(err)
	}
	return v
}
