// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
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

	v := setupViperFlags(t)
	conf, err := config.GetNodeConfig(v)
	require.NoError(err)

	logFactory := logging.NewFactory(conf.LoggingConfig)
	node, err := New(&conf, logFactory, logging.NoLog{})
	require.NoError(err)

	require.IsType(&localsigner.LocalSigner{}, node.StakingSigner)
	wantPk := node.StakingSigner.PublicKey()
	node.Shutdown(0)

	logFactory = logging.NewFactory(conf.LoggingConfig)
	node, err = New(&conf, logFactory, logging.NoLog{})
	require.NoError(err)

	gotPk := node.StakingSigner.PublicKey()
	node.Shutdown(0)

	require.Equal(wantPk, gotPk)
}

// setups config json file and writes content

func setupViperFlags(t *testing.T) *viper.Viper {
	v := viper.New()
	fs := config.BuildFlagSet()
	pflag.Parse()
	if err := v.BindPFlags(fs); err != nil {
		require.NoError(t, err)
	}
	return v
}
