// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestDefaultConfigInitializationUsesExistingDefaultKey(t *testing.T) {
	require := require.New(t)
	root := t.TempDir()

	configFilePath := setupConfigJSON(t, root, "{}")
	v := setupViper(configFilePath)
	conf, err := config.GetNodeConfig(v)
	require.NoError(err)

	logFactory1 := logging.NewFactory(conf.LoggingConfig)
	logger1, err := logFactory1.Make("test1")
	require.NoError(err)

	node1, err := New(&conf, logFactory1, logger1)
	require.NoError(err)

	require.IsType(&localsigner.LocalSigner{}, node1.StakingSigner)
	publicKey1 := node1.StakingSigner.PublicKey()
	node1.Shutdown(0)

	logFactory2 := logging.NewFactory(conf.LoggingConfig)
	logger2, err := logFactory2.Make("test2")
	require.NoError(err)

	node2, err := New(&conf, logFactory2, logger2)
	require.NoError(err)

	require.IsType(&localsigner.LocalSigner{}, node2.StakingSigner)
	publicKey2 := node2.StakingSigner.PublicKey()
	node2.Shutdown(0)

	require.Equal(publicKey1, publicKey2, "Public keys should match for the same default signer config")
}

// setups config json file and writes content
func setupConfigJSON(t *testing.T, rootPath string, value string) string {
	configFilePath := filepath.Join(rootPath, "config.json")
	require.NoError(t, os.WriteFile(configFilePath, []byte(value), 0o600))
	return configFilePath
}

func setupViperFlags() *viper.Viper {
	v := viper.New()
	fs := config.BuildFlagSet()
	pflag.Parse()
	if err := v.BindPFlags(fs); err != nil {
		log.Fatal(err)
	}
	return v
}

func setupViper(configFilePath string) *viper.Viper {
	v := setupViperFlags()
	v.SetConfigFile(configFilePath)
	if err := v.ReadInConfig(); err != nil {
		log.Fatal(err)
	}
	return v
}
