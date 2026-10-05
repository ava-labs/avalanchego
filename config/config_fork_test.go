// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/fork"
	"github.com/ava-labs/avalanchego/fork/forktest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func TestGetForkConfig(t *testing.T) {
	cfg := forktest.NewConfig(t, time.Date(2026, time.October, 15, 15, 0, 0, 0, time.UTC), ids.GenerateTestNodeID())
	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err, "json.Marshal()")
	content := base64.StdEncoding.EncodeToString(cfgJSON)

	t.Run("unset", func(t *testing.T) {
		got, err := getForkConfig(viper.New(), true)
		require.NoError(t, err, "getForkConfig()")
		require.Nil(t, got, "getForkConfig()")
	})
	t.Run("content", func(t *testing.T) {
		v := viper.New()
		v.Set(ForkConfigFileContentKey, content)
		got, err := getForkConfig(v, true)
		require.NoError(t, err, "getForkConfig()")
		require.Equal(t, cfg.Hash(), got.Hash(), "Hash()")
	})
	t.Run("requires sybil protection", func(t *testing.T) {
		v := viper.New()
		v.Set(ForkConfigFileContentKey, content)
		_, err := getForkConfig(v, false)
		require.ErrorIs(t, err, errForkRequiresSybilProtection, "getForkConfig()")
	})
}

func TestGetUpgradeConfigForkMode(t *testing.T) {
	defaults := upgrade.GetConfig(constants.MainnetID)
	forkCfg := forktest.NewConfig(t, defaults.HeliconTime.Add(-time.Hour), ids.GenerateTestNodeID())

	upgradeContent := func(t *testing.T, c upgrade.Config) string {
		b, err := json.Marshal(c)
		require.NoError(t, err, "json.Marshal(upgrade)")
		return base64.StdEncoding.EncodeToString(b)
	}

	t.Run("rejected without fork mode", func(t *testing.T) {
		v := viper.New()
		v.Set(UpgradeFileContentKey, upgradeContent(t, defaults))
		_, err := getUpgradeConfig(v, constants.MainnetID, nil)
		require.ErrorIs(t, err, errCannotConfigureUpgrades, "getUpgradeConfig()")
	})
	t.Run("future upgrade moved in fork mode", func(t *testing.T) {
		override := defaults
		override.HeliconTime = override.HeliconTime.Add(24 * time.Hour)
		v := viper.New()
		v.Set(UpgradeFileContentKey, upgradeContent(t, override))
		got, err := getUpgradeConfig(v, constants.MainnetID, forkCfg)
		require.NoError(t, err, "getUpgradeConfig()")
		require.True(t, override.HeliconTime.Equal(got.HeliconTime), "HeliconTime")
	})
	t.Run("pre-fork upgrade changed in fork mode", func(t *testing.T) {
		override := defaults
		override.DurangoTime = forkCfg.Time.Add(time.Hour)
		v := viper.New()
		v.Set(UpgradeFileContentKey, upgradeContent(t, override))
		_, err := getUpgradeConfig(v, constants.MainnetID, forkCfg)
		require.ErrorIs(t, err, fork.ErrUpgradeBeforeFork, "getUpgradeConfig()")
	})
}
