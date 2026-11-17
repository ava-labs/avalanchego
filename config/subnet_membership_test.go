// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking/stakingtest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/perms"
)

// TestGetSubnetConfigsMembership checks that the subnet config file's
// membership CA, as a path or inline, reaches the subnet's config and loads.
func TestGetSubnetConfigsMembership(t *testing.T) {
	subnetID, err := ids.FromString("2Ctt6eGAeo4MLqTmGa7AdRecuVMPGWEX9wSsCLBYrLhX4a394i")
	require.NoError(t, err)

	root, _ := stakingtest.NewPKI(t)
	caPath := filepath.Join(t.TempDir(), "membership-ca.pem")
	require.NoError(t, os.WriteFile(caPath, root.CertPEM(), perms.ReadWrite))
	inlineCA, err := json.Marshal([]string{string(root.CertPEM())})
	require.NoError(t, err)

	tests := map[string]string{
		"membershipCAPath": fmt.Sprintf(`{"validatorOnly": true, "membershipCAPath": %q}`, caPath),
		"membershipCA":     fmt.Sprintf(`{"validatorOnly": true, "membershipCA": %s}`, inlineCA),
	}
	for name, subnetJSON := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			dir := t.TempDir()
			subnetPath := filepath.Join(dir, "subnets")
			configFilePath := setupConfigJSON(t, dir, fmt.Sprintf(`{%q: %q}`, SubnetConfigDirKey, subnetPath))
			setupFile(t, subnetPath, subnetID.String()+".json", subnetJSON)

			subnetConfigs, err := getSubnetConfigs(setupViper(configFilePath), []ids.ID{subnetID})
			require.NoError(err)
			config := subnetConfigs[subnetID]
			ca, err := config.LoadMembershipCA()
			require.NoError(err)
			require.NotNil(ca)
		})
	}
}

// TestGetNodeConfigNetworkSubnetConfigs checks that the network layer is given
// the subnet configs, which it loads the membership CAs from. Without them a
// node starts cleanly and admits no certificate member, which no other unit
// test would notice.
func TestGetNodeConfigNetworkSubnetConfigs(t *testing.T) {
	require := require.New(t)

	subnetID := ids.GenerateTestID()
	root, _ := stakingtest.NewPKI(t)
	inlineCA, err := json.Marshal([]string{string(root.CertPEM())})
	require.NoError(err)

	v := setupViperFlags()
	v.Set(NetworkNameKey, constants.UnitTestName)
	v.Set(DataDirKey, t.TempDir())
	v.Set(TrackSubnetsKey, subnetID.String())
	v.Set(SubnetConfigContentKey, base64.StdEncoding.EncodeToString(
		fmt.Appendf(nil, `{%q: {"validatorOnly": true, "membershipCA": %s}}`, subnetID, inlineCA),
	))

	config, err := GetNodeConfig(v)
	require.NoError(err)
	require.Equal(config.SubnetConfigs, config.NetworkConfig.SubnetConfigs)
	require.NotEmpty(config.NetworkConfig.SubnetConfigs[subnetID].MembershipCA)
}
