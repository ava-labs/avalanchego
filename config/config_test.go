// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
)

func TestGetChainConfigsFromFiles(t *testing.T) {
	tests := map[string]struct {
		configs    map[string]string
		upgrades   map[string]string
		errMessage string
		expected   map[string]chains.ChainConfig
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
			configJSON := fmt.Sprintf(`{%q: %q}`, ChainConfigDirKey, root)
			configFile := setupConfigJSON(t, root, configJSON)
			chainsDir := root
			// Create custom configs
			for key, value := range test.configs {
				chainDir := filepath.Join(chainsDir, key)
				setupFile(t, chainDir, chainConfigFileName+".ex", value)
			}
			for key, value := range test.upgrades {
				chainDir := filepath.Join(chainsDir, key)
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

func TestGetChainConfigsDirNotExist(t *testing.T) {
	tests := map[string]struct {
		structure  string
		file       map[string]string
		errMessage string
		expected   map[string]chains.ChainConfig
	}{
		"cdir not exist": {
			structure:  "/",
			file:       map[string]string{"config.ex": "noeffect"},
			errMessage: "cannot read directory",
			expected:   nil,
		},
		"cdir is file ": {
			structure:  "/",
			file:       map[string]string{"cdir": "noeffect"},
			errMessage: "cannot read directory",
			expected:   nil,
		},
		"chain subdir not exist": {
			structure: "/cdir/",
			file:      map[string]string{"config.ex": "noeffect"},
			expected:  map[string]chains.ChainConfig{},
		},
		"full structure": {
			structure: "/cdir/C/",
			file:      map[string]string{"config.ex": "hello"},
			expected:  map[string]chains.ChainConfig{"C": {Config: []byte("hello"), Upgrade: []byte(nil)}},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			root := t.TempDir()
			chainConfigDir := filepath.Join(root, "cdir")
			configJSON := fmt.Sprintf(`{%q: %q}`, ChainConfigDirKey, chainConfigDir)
			configFile := setupConfigJSON(t, root, configJSON)

			dirToCreate := filepath.Join(root, test.structure)
			assert.NoError(os.MkdirAll(dirToCreate, 0o700))

			for key, value := range test.file {
				setupFile(t, dirToCreate, key, value)
			}
			v := setupViper(configFile)

			// Parse config
			assert.Equal(chainConfigDir, v.GetString(ChainConfigDirKey))

			// don't read with getConfigFromViper since it's very slow.
			chainConfigs, err := getChainConfigs(v)
			switch {
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
	defaultChainConfigDir = filepath.Join(root, "cdir")
	configFilePath := setupConfigJSON(t, root, "{}")

	v := setupViper(configFilePath)
	assert.Equal(defaultChainConfigDir, v.GetString(ChainConfigDirKey))

	chainsDir := filepath.Join(defaultChainConfigDir, "C")
	setupFile(t, chainsDir, chainConfigFileName+".ex", "helloworld")
	chainConfigs, err := getChainConfigs(v)
	assert.NoError(err)
	expected := map[string]chains.ChainConfig{"C": {Config: []byte("helloworld"), Upgrade: []byte(nil)}}
	assert.Equal(expected, chainConfigs)
}

func TestGetChainConfigsFromFlags(t *testing.T) {
	tests := map[string]struct {
		fullConfigs map[string]chains.ChainConfig
		errMessage  string
		expected    map[string]chains.ChainConfig
	}{
		"no chain configs": {
			fullConfigs: map[string]chains.ChainConfig{},
			expected:    map[string]chains.ChainConfig{},
		},
		"valid chain-id": {
			fullConfigs: func() map[string]chains.ChainConfig {
				m := map[string]chains.ChainConfig{}
				id1, err := ids.FromString("yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp")
				assert.NoError(t, err)
				m[id1.String()] = chains.ChainConfig{Config: []byte("hello"), Upgrade: []byte("helloUpgrades")}

				id2, err := ids.FromString("2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm")
				assert.NoError(t, err)
				m[id2.String()] = chains.ChainConfig{Config: []byte("world"), Upgrade: []byte(nil)}

				return m
			}(),
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
			fullConfigs: map[string]chains.ChainConfig{
				"C": {Config: []byte("hello"), Upgrade: []byte("upgradess")},
				"X": {Config: []byte("world"), Upgrade: []byte(nil)},
			},
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
			jsonMaps, err := json.Marshal(test.fullConfigs)
			assert.NoError(err)
			encodedFileContent := base64.StdEncoding.EncodeToString(jsonMaps)

			// build viper config
			v := setupViperFlags()
			v.Set(ChainConfigContentKey, encodedFileContent)

			// Parse config
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

func TestGetVMAliasesFromFile(t *testing.T) {
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
			aliasPath := filepath.Join(root, "aliases.json")
			configJSON := fmt.Sprintf(`{%q: %q}`, VMAliasesFileKey, aliasPath)
			configFilePath := setupConfigJSON(t, root, configJSON)
			setupFile(t, root, "aliases.json", test.givenJSON)
			v := setupViper(configFilePath)
			vmAliases, err := getVMAliases(v)
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

func TestGetVMAliasesFromFlag(t *testing.T) {
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
			encodedFileContent := base64.StdEncoding.EncodeToString([]byte(test.givenJSON))

			// build viper config
			v := setupViperFlags()
			v.Set(VMAliasesContentKey, encodedFileContent)

			vmAliases, err := getVMAliases(v)
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

func TestGetVMAliasesDefaultDir(t *testing.T) {
	assert := assert.New(t)
	root := t.TempDir()
	// changes internal package variable, since using defaultDir (under user home) is risky.
	defaultVMAliasFilePath = filepath.Join(root, "aliases.json")
	configFilePath := setupConfigJSON(t, root, "{}")

	v := setupViper(configFilePath)
	assert.Equal(defaultVMAliasFilePath, v.GetString(VMAliasesFileKey))

	setupFile(t, root, "aliases.json", `{"2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i": ["vm1","vm2"]}`)
	vmAliases, err := getVMAliases(v)
	assert.NoError(err)

	expected := map[ids.ID][]string{}
	id, _ := ids.FromString("2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i")
	expected[id] = []string{"vm1", "vm2"}
	assert.Equal(expected, vmAliases)
}

func TestGetVMAliasesDirNotExists(t *testing.T) {
	assert := assert.New(t)
	root := t.TempDir()
	aliasPath := "/not/exists"
	// set it explicitly
	configJSON := fmt.Sprintf(`{%q: %q}`, VMAliasesFileKey, aliasPath)
	configFilePath := setupConfigJSON(t, root, configJSON)
	v := setupViper(configFilePath)
	vmAliases, err := getVMAliases(v)
	assert.Nil(vmAliases)
	assert.Error(err)
	assert.Contains(err.Error(), "vm alias file does not exist")

	// do not set it explicitly
	configJSON = "{}"
	configFilePath = setupConfigJSON(t, root, configJSON)
	v = setupViper(configFilePath)
	vmAliases, err = getVMAliases(v)
	assert.Nil(vmAliases)
	assert.NoError(err)
}

func TestGetSubnetConfigsFromFile(t *testing.T) {
	tests := map[string]struct {
		givenJSON  string
		testF      func(*assert.Assertions, map[ids.ID]chains.SubnetConfig)
		errMessage string
		fileName   string
	}{
		"wrong config": {
			fileName:  "2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i.json",
			givenJSON: `thisisnotjson`,
			testF: func(assert *assert.Assertions, given map[ids.ID]chains.SubnetConfig) {
				assert.Nil(given)
			},
			errMessage: "couldn't read subnet configs",
		},
		"subnet is not whitelisted": {
			fileName:  "Gmt4fuNsGJAd2PX86LBvycGaBpgCYKbuULdCLZs3SEs1Jx1LU.json",
			givenJSON: `{"validatorOnly": true}`,
			testF: func(assert *assert.Assertions, given map[ids.ID]chains.SubnetConfig) {
				assert.Empty(given)
			},
		},
		"wrong extension": {
			fileName:  "2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i.yaml",
			givenJSON: `{"validatorOnly": true}`,
			testF: func(assert *assert.Assertions, given map[ids.ID]chains.SubnetConfig) {
				assert.Empty(given)
			},
		},
		"invalid consensus parameters": {
			fileName:  "2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i.json",
			givenJSON: `{"consensusParameters":{"k": 111, "alpha":1234} }`,
			testF: func(assert *assert.Assertions, given map[ids.ID]chains.SubnetConfig) {
				assert.Nil(given)
			},
			errMessage: "fails the condition that: alpha <= k",
		},
		"correct config": {
			fileName:  "2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i.json",
			givenJSON: `{"validatorOnly": true, "consensusParameters":{"parents": 111, "alpha":16} }`,
			testF: func(assert *assert.Assertions, given map[ids.ID]chains.SubnetConfig) {
				id, _ := ids.FromString("2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i")
				config, ok := given[id]
				assert.True(ok)

				assert.Equal(true, config.ValidatorOnly)
				assert.Equal(111, config.ConsensusParameters.Parents)
				assert.Equal(16, config.ConsensusParameters.Alpha)
				// must still respect defaults
				assert.Equal(20, config.ConsensusParameters.K)
			},
			errMessage: "",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			root := t.TempDir()
			subnetPath := filepath.Join(root, "subnets")
			configJSON := fmt.Sprintf(`{%q: %q}`, SubnetConfigDirKey, subnetPath)
			configFilePath := setupConfigJSON(t, root, configJSON)
			subnetID, err := ids.FromString("2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i")
			assert.NoError(err)
			setupFile(t, subnetPath, test.fileName, test.givenJSON)
			v := setupViper(configFilePath)
			subnetConfigs, err := getSubnetConfigs(v, []ids.ID{subnetID})
			if len(test.errMessage) > 0 {
				assert.Error(err)
				assert.Contains(err.Error(), test.errMessage)
			} else {
				assert.NoError(err)
				test.testF(assert, subnetConfigs)
			}
		})
	}
}

func TestGetSubnetConfigsFromFlags(t *testing.T) {
	tests := map[string]struct {
		cfgsMap    map[ids.ID]chains.SubnetConfig
		testF      func(*assert.Assertions, map[ids.ID]chains.SubnetConfig)
		errMessage string
	}{
		"no configs": {
			cfgsMap: func() map[ids.ID]chains.SubnetConfig {
				res := make(map[ids.ID]chains.SubnetConfig)
				return res
			}(),
			testF: func(assert *assert.Assertions, given map[ids.ID]chains.SubnetConfig) {
				assert.Empty(given)
			},
			errMessage: "",
		},
		"entry with no config": {
			cfgsMap: func() map[ids.ID]chains.SubnetConfig {
				res := make(map[ids.ID]chains.SubnetConfig)
				id, _ := ids.FromString("2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i")
				res[id] = chains.SubnetConfig{}
				return res
			}(),
			testF: func(assert *assert.Assertions, given map[ids.ID]chains.SubnetConfig) {
				assert.True(len(given) == 1)
				id, _ := ids.FromString("2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i")
				_, ok := given[id]
				assert.True(ok)
			},
			errMessage: "Fails the condition that: 1 < Parents",
		},
		"subnet is not whitelisted": {
			cfgsMap: func() map[ids.ID]chains.SubnetConfig {
				res := make(map[ids.ID]chains.SubnetConfig)
				id, _ := ids.FromString("Gmt4fuNsGJAd2PX86LBvycGaBpgCYKbuULdCLZs3SEs1Jx1LU")
				res[id] = chains.SubnetConfig{ValidatorOnly: true}
				return res
			}(),
			testF: func(assert *assert.Assertions, given map[ids.ID]chains.SubnetConfig) {
				assert.Empty(given)
			},
		},
		"invalid consensus parameters": {
			cfgsMap: func() map[ids.ID]chains.SubnetConfig {
				res := make(map[ids.ID]chains.SubnetConfig)
				id, _ := ids.FromString("2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i")
				res[id] = chains.SubnetConfig{
					ConsensusParameters: avalanche.Parameters{
						Parents:   2,
						BatchSize: 1,
						Parameters: snowball.Parameters{
							K:            111,
							Alpha:        1234,
							BetaVirtuous: 1,
						},
					},
				}
				return res
			}(),
			testF: func(assert *assert.Assertions, given map[ids.ID]chains.SubnetConfig) {
				assert.Empty(given)
			},
			errMessage: "fails the condition that: alpha <= k",
		},
		"correct config": {
			cfgsMap: func() map[ids.ID]chains.SubnetConfig {
				res := make(map[ids.ID]chains.SubnetConfig)
				id, _ := ids.FromString("2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i")
				res[id] = chains.SubnetConfig{
					ValidatorOnly: true,
					ConsensusParameters: avalanche.Parameters{
						Parents:   111,
						BatchSize: 1,
						Parameters: snowball.Parameters{
							Alpha:                 20,
							K:                     30,
							BetaVirtuous:          5,
							BetaRogue:             6,
							ConcurrentRepolls:     6,
							OptimalProcessing:     2,
							MaxOutstandingItems:   2,
							MaxItemProcessingTime: 2,
						},
					},
				}
				return res
			}(),
			testF: func(assert *assert.Assertions, given map[ids.ID]chains.SubnetConfig) {
				id, _ := ids.FromString("2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i")
				config, ok := given[id]
				assert.True(ok)
				assert.Equal(true, config.ValidatorOnly)
				assert.Equal(111, config.ConsensusParameters.Parents)
				assert.Equal(20, config.ConsensusParameters.Alpha)
				// must still respect defaults
				assert.Equal(30, config.ConsensusParameters.K)
			},
			errMessage: "",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			subnetID, err := ids.FromString("2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i")
			assert.NoError(err)
			cfgsMapBytes, err := json.Marshal(test.cfgsMap)
			assert.NoError(err)
			encodedFileContent := base64.StdEncoding.EncodeToString(cfgsMapBytes)

			// build viper config
			v := setupViperFlags()
			v.Set(SubnetConfigContentKey, encodedFileContent)

			// setup configs for default
			v.Set(SnowAvalancheNumParentsKey, 1)
			v.Set(SnowAvalancheBatchSizeKey, 1)

			subnetConfigs, err := getSubnetConfigs(v, []ids.ID{subnetID})
			if len(test.errMessage) > 0 {
				assert.Error(err)
				assert.Contains(err.Error(), test.errMessage)
			} else {
				assert.NoError(err)
				test.testF(assert, subnetConfigs)
			}
		})
	}
}

// setups config json file and writes content
func setupConfigJSON(t *testing.T, rootPath string, value string) string {
	configFilePath := filepath.Join(rootPath, "config.json")
	assert.NoError(t, ioutil.WriteFile(configFilePath, []byte(value), 0o600))
	return configFilePath
}

// setups file creates necessary path and writes value to it.
func setupFile(t *testing.T, path string, fileName string, value string) {
	assert.NoError(t, os.MkdirAll(path, 0o700))
	filePath := filepath.Join(path, fileName)
	assert.NoError(t, ioutil.WriteFile(filePath, []byte(value), 0o600))
}

func setupViperFlags() *viper.Viper {
	v := viper.New()
	fs := BuildFlagSet()
	pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.PanicOnError) // flags are now reset
	pflag.CommandLine.AddGoFlagSet(fs)
	pflag.Parse()
	if err := v.BindPFlags(pflag.CommandLine); err != nil {
		log.Fatal(err)
	}
	return v
}

func setupViper(configFilePath string) *viper.Viper {
	v := setupViperFlags()
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
