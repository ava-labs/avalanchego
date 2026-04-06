// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestNetworkSerialization(t *testing.T) {
	require := require.New(t)

	tmpDir := t.TempDir()

	ctx := t.Context()

	network := NewDefaultNetwork("testnet")
	// Runtime configuration is required
	network.DefaultRuntimeConfig.Process = &ProcessRuntimeConfig{}
	// Validate round-tripping of primary subnet configuration
	network.PrimarySubnetConfig = ConfigMap{
		"validatorOnly": true,
	}
	require.NoError(network.EnsureDefaultConfig(ctx, logging.NoLog{}))
	require.NoError(network.Create(tmpDir))
	// Ensure node runtime is initialized
	require.NoError(network.readNodes(ctx))

	loadedNetwork, err := ReadNetwork(ctx, logging.NoLog{}, network.Dir)
	require.NoError(err)
	for _, key := range loadedNetwork.PreFundedKeys {
		// Address() enables comparison with the original network by
		// ensuring full population of a key's in-memory representation.
		_ = key.Address()
	}
	require.Equal(network, loadedNetwork)
}

func TestCopyArchivedNodeStateExcludesTransientArtifacts(t *testing.T) {
	require := require.New(t)

	srcDir := t.TempDir()
	destDir := t.TempDir()

	require.NoError(os.WriteFile(filepath.Join(srcDir, defaultConfigFilename), []byte("config"), 0o644))
	require.NoError(os.WriteFile(filepath.Join(srcDir, "flags.json"), []byte("flags"), 0o644))
	require.NoError(os.WriteFile(filepath.Join(srcDir, config.DefaultProcessContextFilename), []byte("process"), 0o644))
	require.NoError(os.MkdirAll(filepath.Join(srcDir, "db"), 0o755))
	require.NoError(os.WriteFile(filepath.Join(srcDir, "db", "state"), []byte("state"), 0o644))
	require.NoError(os.MkdirAll(filepath.Join(srcDir, "logs"), 0o755))
	require.NoError(os.WriteFile(filepath.Join(srcDir, "logs", "main.log"), []byte("logs"), 0o644))
	require.NoError(os.MkdirAll(filepath.Join(srcDir, "metrics"), 0o755))
	require.NoError(os.WriteFile(filepath.Join(srcDir, "metrics", "snapshot"), []byte("metrics"), 0o644))

	require.NoError(copyArchivedNodeState(srcDir, destDir))

	_, err := os.Stat(filepath.Join(destDir, defaultConfigFilename))
	require.ErrorIs(err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(destDir, "flags.json"))
	require.ErrorIs(err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(destDir, "db", "state"))
	require.NoError(err)
	_, err = os.Stat(filepath.Join(destDir, config.DefaultProcessContextFilename))
	require.ErrorIs(err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(destDir, "logs"))
	require.ErrorIs(err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(destDir, "metrics"))
	require.ErrorIs(err, os.ErrNotExist)
}

func TestExportNetworkArchiveRejectsRunningNetwork(t *testing.T) {
	require := require.New(t)
	ctx := t.Context()

	network := &Network{
		Owner: "testnet",
		Nodes: NewNodesOrPanic(1),
		DefaultRuntimeConfig: NodeRuntimeConfig{
			Process: &ProcessRuntimeConfig{},
		},
	}
	require.NoError(network.EnsureDefaultConfig(ctx, logging.NoLog{}))
	require.NoError(network.Create(t.TempDir()))

	processContextPath := filepath.Join(network.Nodes[0].DataDir, config.DefaultProcessContextFilename)
	require.NoError(os.WriteFile(processContextPath, []byte(`{"uri":"http://127.0.0.1:9650"}`), 0o644))

	err := ExportNetworkArchive(ctx, logging.NoLog{}, network.Dir, filepath.Join(t.TempDir(), "network.tar.gz"))
	require.ErrorIs(err, errExportRunningNetwork)
}

func TestGetArchiveableNodesRejectsUnsupportedRuntime(t *testing.T) {
	require := require.New(t)

	network := &Network{
		DefaultRuntimeConfig: NodeRuntimeConfig{
			Kube: &KubeRuntimeConfig{},
		},
		Nodes: []*Node{NewNode()},
	}
	network.Nodes[0].network = network
	persistentNodes, err := getArchiveableNodes(network, false, true)
	require.Nil(persistentNodes)
	require.ErrorIs(err, errArchiveUnsupportedRuntime)
}

func TestGetArchiveableNodesRejectsAllEphemeralNetworks(t *testing.T) {
	require := require.New(t)

	network := &Network{
		DefaultRuntimeConfig: NodeRuntimeConfig{
			Process: &ProcessRuntimeConfig{},
		},
		Nodes: []*Node{NewEphemeralNode(FlagsMap{})},
	}
	network.Nodes[0].network = network
	persistentNodes, err := getArchiveableNodes(network, false, true)
	require.Nil(persistentNodes)
	require.ErrorIs(err, errNoPersistentNodes)
}

func TestImportNetworkArchiveClearsExplicitDataDir(t *testing.T) {
	require := require.New(t)
	ctx := t.Context()

	network := &Network{
		Owner: "testnet",
		Nodes: NewNodesOrPanic(1),
		DefaultRuntimeConfig: NodeRuntimeConfig{
			Process: &ProcessRuntimeConfig{},
		},
	}
	require.NoError(network.EnsureDefaultConfig(ctx, logging.NoLog{}))

	rootDir := t.TempDir()
	require.NoError(network.Create(rootDir))

	defaultDataDir := network.Nodes[0].DataDir
	customDataDir := filepath.Join(network.Dir, "custom-node-dir")
	network.Nodes[0].Flags[config.DataDirKey] = customDataDir
	network.Nodes[0].DataDir = customDataDir
	require.NoError(network.Nodes[0].Write())
	require.NoError(os.RemoveAll(defaultDataDir))

	require.NoError(os.MkdirAll(filepath.Join(customDataDir, "db"), 0o755))
	require.NoError(os.WriteFile(filepath.Join(customDataDir, "db", "state"), []byte("state"), 0o644))

	archivePath := filepath.Join(t.TempDir(), "network.tar.gz")
	require.NoError(ExportNetworkArchive(ctx, logging.NoLog{}, network.Dir, archivePath))

	importedNetwork, err := ImportNetworkArchive(ctx, logging.NoLog{}, archivePath, t.TempDir(), &network.DefaultRuntimeConfig)
	require.NoError(err)
	require.Len(importedNetwork.Nodes, 1)
	require.NotContains(importedNetwork.Nodes[0].Flags, config.DataDirKey)
	require.Equal(filepath.Join(importedNetwork.Dir, importedNetwork.Nodes[0].NodeID.String()), importedNetwork.Nodes[0].DataDir)

	stateBytes, err := os.ReadFile(filepath.Join(importedNetwork.Nodes[0].DataDir, "db", "state"))
	require.NoError(err)
	require.Equal([]byte("state"), stateBytes)
}

func TestImportNetworkArchiveRoundTripsSubnets(t *testing.T) {
	require := require.New(t)
	ctx := t.Context()

	network := &Network{
		Owner: "testnet",
		Nodes: NewNodesOrPanic(1),
		DefaultRuntimeConfig: NodeRuntimeConfig{
			Process: &ProcessRuntimeConfig{},
		},
	}
	require.NoError(network.EnsureDefaultConfig(ctx, logging.NoLog{}))
	require.NoError(network.Create(t.TempDir()))

	subnet := &Subnet{
		Name: "subnet-a",
		Config: ConfigMap{
			"validatorOnly": true,
		},
	}
	require.NoError(subnet.Write(network.GetSubnetDir()))

	archivePath := filepath.Join(t.TempDir(), "network.tar.gz")
	require.NoError(ExportNetworkArchive(ctx, logging.NoLog{}, network.Dir, archivePath))

	importedNetwork, err := ImportNetworkArchive(ctx, logging.NoLog{}, archivePath, t.TempDir(), &network.DefaultRuntimeConfig)
	require.NoError(err)
	require.Len(importedNetwork.Subnets, 1)
	require.Equal(subnet.Name, importedNetwork.Subnets[0].Name)
	require.Equal(subnet.Config, importedNetwork.Subnets[0].Config)
	_, err = os.Stat(filepath.Join(importedNetwork.GetSubnetDir(), subnet.Name+jsonFileSuffix))
	require.NoError(err)
}
