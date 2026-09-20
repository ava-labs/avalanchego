// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"maps"
	"net/netip"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
)

const (
	archiveManifestFilename = "archive.json"
	archiveStateDirName     = "state"
	archiveDatabaseDirName  = "db"
	archiveChainDataDirName = "chainData"
	// Increment when an archive layout change is not backward compatible.
	archiveFormatVersion = 3
)

var (
	errNoPersistentNodes         = errors.New("network archive requires at least one non-ephemeral node")
	errExportRunningNetwork      = errors.New("network archive export requires all nodes to be stopped")
	errArchiveUnsupportedRuntime = errors.New("network archive export supports only process-backed persistent nodes")
	errImportRuntimeRequired     = errors.New("network archive import requires runtime configuration")
	errImportUnsupportedRuntime  = errors.New("network archive import supports only process-backed nodes")
	errUnsupportedArchiveFormat  = errors.New("unsupported network archive format")
	errIncompatibleArchiveDB     = errors.New("incompatible network archive database version")
	errArchiveInconsistentDB     = errors.New("network archive requires all nodes to use the same database version")
	errMissingArchiveDB          = errors.New("AvalancheGo version output is missing the database version")
	errInvalidArchiveEntry       = errors.New("invalid archive entry")
)

type archiveManifest struct {
	FormatVersion   int    `json:"formatVersion"`
	DatabaseVersion string `json:"databaseVersion"`
}

// ExportNetworkArchive writes a restartable archive for a stopped tmpnet network.
// The archive contains network configuration, per-node configuration, and one shared
// persistent-state directory from a non-ephemeral node.
// Export rejects running networks, networks without any non-ephemeral nodes, and networks
// whose persistent nodes use unsupported runtimes. The archive records its format and
// database versions. Runtime configuration is intentionally not preserved so imports can
// be rebound to local execution settings.
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
	databaseVersion, err := getArchiveDatabaseVersion(log, persistentNodes)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	if err := writeArchiveManifest(stagingRoot, databaseVersion); err != nil {
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
// derived under the new network directory, copies the archived shared persistent state to each
// node, binds the imported network to the provided local process runtime configuration, and does
// not start any nodes.
func ImportNetworkArchive(ctx context.Context, log logging.Logger, archivePath string, rootNetworkDir string, runtimeConfig *NodeRuntimeConfig) (*Network, error) {
	if runtimeConfig == nil {
		return nil, stacktrace.Wrap(errImportRuntimeRequired)
	}
	if runtimeConfig.Process == nil {
		return nil, stacktrace.Wrap(errImportUnsupportedRuntime)
	}
	extractedRoot, err := os.MkdirTemp("", "tmpnet-import-*")
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}
	defer os.RemoveAll(extractedRoot)

	if err := extractTarGz(archivePath, extractedRoot); err != nil {
		return nil, stacktrace.Wrap(err)
	}
	manifest, err := readArchiveManifest(extractedRoot)
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}
	targetVersion, err := getAvalancheGoVersion(log, runtimeConfig.Process.AvalancheGoPath)
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}
	if err := validateArchiveDatabaseVersion(manifest, targetVersion.Database); err != nil {
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

	for _, node := range persistentNodes {
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
	archiveStateDir := filepath.Join(extractedRoot, archiveStateDirName)
	for _, node := range network.Nodes {
		if err := copyImportedNodeState(archiveStateDir, node.DataDir); err != nil {
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

func getArchiveDatabaseVersion(log logging.Logger, nodes []*Node) (string, error) {
	var databaseVersion string
	for _, node := range nodes {
		versions, err := getAvalancheGoVersion(log, node.getRuntimeConfig().Process.AvalancheGoPath)
		if err != nil {
			return "", stacktrace.Wrap(err)
		}
		if versions.Database == "" {
			return "", stacktrace.Wrap(errMissingArchiveDB)
		}
		if databaseVersion == "" {
			databaseVersion = versions.Database
			continue
		}
		if databaseVersion != versions.Database {
			return "", stacktrace.Errorf("%w: got %q and %q", errArchiveInconsistentDB, databaseVersion, versions.Database)
		}
	}
	return databaseVersion, nil
}

func writeArchiveManifest(destRoot string, databaseVersion string) error {
	manifestBytes, err := json.Marshal(archiveManifest{
		FormatVersion:   archiveFormatVersion,
		DatabaseVersion: databaseVersion,
	})
	if err != nil {
		return stacktrace.Wrap(err)
	}
	return stacktrace.Wrap(os.WriteFile(filepath.Join(destRoot, archiveManifestFilename), manifestBytes, perms.ReadWrite))
}

func readArchiveManifest(root string) (*archiveManifest, error) {
	manifestBytes, err := os.ReadFile(filepath.Join(root, archiveManifestFilename))
	if err != nil {
		return nil, stacktrace.Errorf("%w: missing manifest: %w", errUnsupportedArchiveFormat, err)
	}

	manifest := archiveManifest{}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, stacktrace.Errorf("%w: invalid manifest: %w", errUnsupportedArchiveFormat, err)
	}
	if manifest.FormatVersion != archiveFormatVersion {
		return nil, stacktrace.Errorf("%w: got version %d, expected %d", errUnsupportedArchiveFormat, manifest.FormatVersion, archiveFormatVersion)
	}
	return &manifest, nil
}

func validateArchiveDatabaseVersion(manifest *archiveManifest, databaseVersion string) error {
	if databaseVersion == "" {
		return stacktrace.Wrap(errMissingArchiveDB)
	}
	if manifest.DatabaseVersion != databaseVersion {
		return stacktrace.Errorf("%w: got version %q, expected %q", errIncompatibleArchiveDB, manifest.DatabaseVersion, databaseVersion)
	}
	return nil
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
	}

	// The chain state is shared across persistent nodes. Archive it once and copy it to
	// each newly materialized node during import.
	return copyArchivedNodeState(nodes[0].DataDir, filepath.Join(destRoot, archiveStateDirName))
}

func copyArchivedNodeState(srcDir string, destDir string) error {
	return copyPersistentNodeState(srcDir, destDir)
}

func copyImportedNodeState(srcDir string, destDir string) error {
	return copyPersistentNodeState(srcDir, destDir)
}

func copyPersistentNodeState(srcDir string, destDir string) error {
	for _, name := range []string{archiveDatabaseDirName, archiveChainDataDirName} {
		if err := copyDirIfExists(filepath.Join(srcDir, name), filepath.Join(destDir, name), nil); err != nil {
			return stacktrace.Wrap(err)
		}
	}
	return nil
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

	gzipWriter := gzip.NewWriter(archiveFile)
	tarWriter := tar.NewWriter(gzipWriter)

	walkErr := filepath.Walk(srcDir, func(path string, info fs.FileInfo, walkErr error) error {
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
	if err := errors.Join(walkErr, tarWriter.Close(), gzipWriter.Close(), archiveFile.Close()); err != nil {
		return stacktrace.Wrap(err)
	}
	return nil
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
		if cleanName == "." || !filepath.IsLocal(cleanName) {
			return stacktrace.Errorf("%w: %q", errInvalidArchiveEntry, header.Name)
		}
		destPath := filepath.Join(destDir, cleanName)

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
