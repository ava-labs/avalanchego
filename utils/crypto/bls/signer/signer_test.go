package signer

import (
	"log"
	"testing"

	"github.com/ava-labs/avalanchego/config"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfigInitializationUsesExistingDefaultKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	require := require.New(t)
	v := setupViperFlags()

	config1, err := config.GetNodeConfig(v)
	require.NoError(err)
	signer1, err := GetStakingSigner(config1.StakingSignerConfig)
	require.NoError(err)
	signer2, err := GetStakingSigner(config1.StakingSignerConfig)
	require.NoError(err)

	require.Equal(signer1.PublicKey(), signer2.PublicKey())
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
