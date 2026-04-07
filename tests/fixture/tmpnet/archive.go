// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"io/fs"
	"maps"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
)

var (
	errNoPersistentNodes         = errors.New("network archive requires at least one non-ephemeral node")
	errExportRunningNetwork      = errors.New("network archive export requires all nodes to be stopped")
	errArchiveUnsupportedRuntime = errors.New("network archive export supports only process-backed persistent nodes")
	errImportRuntimeRequired     = errors.New("network archive import requires runtime configuration")
)

// ExportNetworkArchive writes a restartable archive for a stopped tmpnet network.
// The archive contains network configuration and only non-ephemeral node state.
// Export rejects running networks, networks without any non-ephemeral nodes, and networks
// whose persistent nodes use unsupported runtimes. Runtime configuration is intentionally
// not preserved in the archive so imports can be rebound to local execution settings.
func ExportNetworkArchive(ctx context.Context, log logging.Logger, networkDir string, archivePath string) error {
	network, err := ReadNetwork(ctx, log, networkDir)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	persistentNodes, err := getArchiveableNodes(network, true, true)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	stagingRoot, err := os.MkdirTemp("", "tmpnet-export-*")
	if err != nil {
		return stacktrace.Wrap(err)
	}
	defer os.RemoveAll(stagingRoot)

	if err := copyNetworkArchiveRoot(network, stagingRoot, persistentNodes); err != nil {
		return stacktrace.Wrap(err)
	}
	if err := writeTarGz(stagingRoot, archivePath); err != nil {
		return stacktrace.Wrap(err)
	}
	return nil
}

// ImportNetworkArchive materializes a fresh tmpnet network from an exported archive.
// Import assigns the network a new UUID and network directory, preserves archived persistent
// node identities, clears explicit archived data-dir settings so node paths are freshly
// derived under the new network directory, binds the imported network to the provided local
// runtime configuration, and does not start any nodes.
func ImportNetworkArchive(ctx context.Context, log logging.Logger, archivePath string, rootNetworkDir string, runtimeConfig *NodeRuntimeConfig) (*Network, error) {
	if runtimeConfig == nil {
		return nil, stacktrace.Wrap(errImportRuntimeRequired)
	}
	extractedRoot, err := os.MkdirTemp("", "tmpnet-import-*")
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}
	defer os.RemoveAll(extractedRoot)

	if err := extractTarGz(archivePath, extractedRoot); err != nil {
		return nil, stacktrace.Wrap(err)
	}

	network, err := ReadNetwork(ctx, log, extractedRoot)
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}

	persistentNodes, err := getArchiveableNodes(network, false, false)
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}

	sourceDataDirs := make(map[ids.NodeID]string, len(persistentNodes))
	for _, node := range persistentNodes {
		sourceDataDirs[node.NodeID] = node.DataDir
		delete(node.Flags, config.DataDirKey)
		node.DataDir = ""
		node.URI = ""
		node.StakingAddress = netip.AddrPort{}
		node.runtime = nil
	}

	network.Nodes = persistentNodes
	network.UUID = uuid.NewString()
	network.Dir = ""
	network.log = log
	network.DefaultRuntimeConfig = *runtimeConfig
	if network.DefaultFlags == nil {
		network.DefaultFlags = FlagsMap{}
	}

	if err := network.Create(rootNetworkDir); err != nil {
		return nil, stacktrace.Wrap(err)
	}
	for _, subnet := range network.Subnets {
		if err := subnet.Write(network.GetSubnetDir()); err != nil {
			return nil, stacktrace.Wrap(err)
		}
	}
	for _, node := range network.Nodes {
		if err := copyImportedNodeState(sourceDataDirs[node.NodeID], node.DataDir); err != nil {
			return nil, stacktrace.Wrap(err)
		}
	}

	return ReadNetwork(ctx, log, network.Dir)
}

func getArchiveableNodes(network *Network, requireStopped bool, requireProcessRuntime bool) ([]*Node, error) {
	persistentNodes := make([]*Node, 0, len(network.Nodes))
	for _, node := range network.Nodes {
		if requireStopped && node.IsRunning() {
			return nil, stacktrace.Wrap(errExportRunningNetwork)
		}
		if node.IsEphemeral {
			continue
		}
		if requireProcessRuntime && node.getRuntimeConfig().Process == nil {
			return nil, stacktrace.Wrap(errArchiveUnsupportedRuntime)
		}
		persistentNodes = append(persistentNodes, node)
	}
	if len(persistentNodes) == 0 {
		return nil, stacktrace.Wrap(errNoPersistentNodes)
	}
	return persistentNodes, nil
}

func writeArchivedNetworkConfig(destRoot string, network *Network) error {
	archivedNetwork := *network
	archivedNetwork.Dir = destRoot
	archivedNetwork.DefaultRuntimeConfig = NodeRuntimeConfig{}
	return archivedNetwork.writeNetworkConfig()
}

func writeArchivedNodeConfig(destDir string, node *Node) error {
	archivedFlags := maps.Clone(node.Flags)
	delete(archivedFlags, config.DataDirKey)

	archivedNode := &Node{
		NodeID:      node.NodeID,
		Flags:       archivedFlags,
		IsEphemeral: node.IsEphemeral,
		DataDir:     destDir,
		network:     node.network,
	}
	return archivedNode.Write()
}

func copyNetworkArchiveRoot(network *Network, destRoot string, nodes []*Node) error {
	if err := os.MkdirAll(destRoot, perms.ReadWriteExecute); err != nil {
		return stacktrace.Wrap(err)
	}
	if err := writeArchivedNetworkConfig(destRoot, network); err != nil {
		return stacktrace.Wrap(err)
	}
	if err := copyFileIfExists(network.GetGenesisPath(), filepath.Join(destRoot, filepath.Base(network.GetGenesisPath()))); err != nil {
		return stacktrace.Wrap(err)
	}
	if err := copyDirIfExists(network.GetSubnetDir(), filepath.Join(destRoot, defaultSubnetDirName), nil); err != nil {
		return stacktrace.Wrap(err)
	}
	for _, node := range nodes {
		archivedNodeDir := filepath.Join(destRoot, node.NodeID.String())
		if err := writeArchivedNodeConfig(archivedNodeDir, node); err != nil {
			return stacktrace.Wrap(err)
		}
		if err := copyArchivedNodeState(node.DataDir, archivedNodeDir); err != nil {
			return stacktrace.Wrap(err)
		}
	}
	return nil
}

func copyArchivedNodeState(srcDir string, destDir string) error {
	return copyDirIfExists(srcDir, destDir, func(_ string, entry fs.DirEntry) bool {
		name := entry.Name()
		if entry.IsDir() {
			return name == "logs" || name == "metrics"
		}
		return name == defaultConfigFilename || name == "flags.json" || name == config.DefaultProcessContextFilename
	})
}

func copyImportedNodeState(srcDir string, destDir string) error {
	return copyDirIfExists(srcDir, destDir, func(_ string, entry fs.DirEntry) bool {
		name := entry.Name()
		if entry.IsDir() {
			return name == "logs" || name == "metrics"
		}
		return name == defaultConfigFilename || name == "flags.json" || name == config.DefaultProcessContextFilename
	})
}

func copyFileIfExists(src string, dest string) error {
	info, err := os.Stat(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return stacktrace.Wrap(err)
	}
	if info.IsDir() {
		return stacktrace.Errorf("expected file, found directory: %s", src)
	}
	return copyFile(src, dest, info.Mode())
}

func copyDirIfExists(srcDir string, destDir string, skip func(path string, entry fs.DirEntry) bool) error {
	info, err := os.Stat(srcDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return stacktrace.Wrap(err)
	}
	if !info.IsDir() {
		return stacktrace.Errorf("expected directory, found file: %s", srcDir)
	}

	return filepath.WalkDir(srcDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return stacktrace.Wrap(walkErr)
		}
		if skip != nil && skip(path, entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return stacktrace.Wrap(err)
		}
		if relPath == "." {
			return os.MkdirAll(destDir, perms.ReadWriteExecute)
		}
		destPath := filepath.Join(destDir, relPath)

		info, err := entry.Info()
		if err != nil {
			return stacktrace.Wrap(err)
		}
		if entry.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}
		if !entry.Type().IsRegular() {
			return stacktrace.Errorf("unsupported archive entry %s (%s)", path, entry.Type())
		}
		return copyFile(path, destPath, info.Mode())
	})
}

func copyFile(src string, dest string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), perms.ReadWriteExecute); err != nil {
		return stacktrace.Wrap(err)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	defer srcFile.Close()

	destFile, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return stacktrace.Wrap(err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return stacktrace.Wrap(err)
	}
	return nil
}

func writeTarGz(srcDir string, archivePath string) error {
	if err := os.MkdirAll(filepath.Dir(archivePath), perms.ReadWriteExecute); err != nil {
		return stacktrace.Wrap(err)
	}

	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	defer archiveFile.Close()

	gzipWriter := gzip.NewWriter(archiveFile)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	return filepath.Walk(srcDir, func(path string, info fs.FileInfo, walkErr error) error {
		if walkErr != nil {
			return stacktrace.Wrap(walkErr)
		}
		if path == srcDir {
			return nil
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return stacktrace.Wrap(err)
		}
		tarPath := filepath.ToSlash(relPath)
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return stacktrace.Wrap(err)
		}
		header.Name = tarPath
		if info.IsDir() {
			header.Name += "/"
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return stacktrace.Wrap(err)
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return stacktrace.Wrap(err)
		}
		defer file.Close()

		if _, err := io.Copy(tarWriter, file); err != nil {
			return stacktrace.Wrap(err)
		}
		return nil
	})
}

func extractTarGz(archivePath string, destDir string) error {
	archiveFile, err := os.Open(archivePath)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	defer archiveFile.Close()

	gzipReader, err := gzip.NewReader(archiveFile)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return stacktrace.Wrap(err)
		}

		cleanName := filepath.Clean(header.Name)
		if cleanName == "." || strings.HasPrefix(cleanName, "..") {
			return stacktrace.Errorf("invalid archive entry %q", header.Name)
		}
		destPath := filepath.Join(destDir, cleanName)
		if !strings.HasPrefix(destPath, destDir+string(os.PathSeparator)) && destPath != destDir {
			return stacktrace.Errorf("archive entry escapes destination: %q", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, fs.FileMode(header.Mode)); err != nil {
				return stacktrace.Wrap(err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(destPath), perms.ReadWriteExecute); err != nil {
				return stacktrace.Wrap(err)
			}
			file, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fs.FileMode(header.Mode))
			if err != nil {
				return stacktrace.Wrap(err)
			}
			if _, err := io.Copy(file, tarReader); err != nil {
				file.Close()
				return stacktrace.Wrap(err)
			}
			if err := file.Close(); err != nil {
				return stacktrace.Wrap(err)
			}
		default:
			return stacktrace.Errorf("unsupported archive entry type %d for %q", header.Typeflag, header.Name)
		}
	}
}
