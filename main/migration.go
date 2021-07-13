package main

import (
	"fmt"
	"path/filepath"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/rocksdb"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/storage"
	"github.com/ava-labs/avalanchego/version"
)

const (
	gb    = 1 << 30
	gb200 = 200 * gb
)

type migrationManager struct {
	nodeManager *nodeManager
	rootConfig  node.Config
	log         logging.Logger
}

func newMigrationManager(nodeManager *nodeManager, rootConfig node.Config, log logging.Logger) *migrationManager {
	return &migrationManager{
		nodeManager: nodeManager,
		rootConfig:  rootConfig,
		log:         log,
	}
}

// Runs migration if required. See runMigration().
func (m *migrationManager) migrate() error {
	shouldMigrate, err := m.shouldMigrate()
	if err != nil {
		return err
	}
	if !shouldMigrate {
		return nil
	}
	err = m.verifyDiskStorage()
	if err != nil {
		return err
	}
	return m.runMigration()
}

func (m *migrationManager) verifyDiskStorage() error {
	storagePath := m.rootConfig.DBPath
	avail, err := storage.OsDiskStat(storagePath)
	if err != nil {
		return err
	}

	currentDBPath := filepath.Join(storagePath, version.CurrentDatabase.String())
	prevDBPath := filepath.Join(storagePath, version.PrevDatabase.String())

	currentUsed, err := storage.DirSize(currentDBPath)
	if err != nil {
		return err
	}
	prevUsed, err := storage.DirSize(prevDBPath)
	if err != nil {
		return err
	}

	safetyBuf := (prevUsed * 30) / 100
	// Total space available to [currentDBPath] is counted as
	// the total storage taken up by that database plus the disk
	// space remaining on the device.
	// Require that total space available to the current database has
	// at least the same amount of space as the previous database plus
	// 30% overhead.
	required := prevUsed + safetyBuf - currentUsed
	if avail < required {
		return fmt.Errorf("available space %dGB is less then required space %dGB for migration",
			avail/gb, required/gb)
	}
	if avail < gb200 {
		m.log.Warn("at least 200GB of free disk space is recommended")
	}
	return nil
}

// Returns true if the database should be migrated from the previous database version.
// Should migrate if the previous database version exists and
// if the latest database version has not finished bootstrapping.
func (m *migrationManager) shouldMigrate() (bool, error) {
	var (
		dbManager manager.Manager
		err       error
	)
	switch m.rootConfig.DBName {
	case rocksdb.Name:
		path := filepath.Join(m.rootConfig.DBPath, "rocksdb")
		dbManager, err = manager.NewRocksDB(path, logging.NoLog{}, version.CurrentDatabase, true)
	case leveldb.Name:
		dbManager, err = manager.NewLevelDB(m.rootConfig.DBPath, logging.NoLog{}, version.CurrentDatabase, true)
	case memdb.Name:
		return false, nil
	default:
		err = fmt.Errorf(
			"db-type was %q but should have been one of {%s, %s, %s}",
			m.rootConfig.DBName,
			leveldb.Name,
			rocksdb.Name,
			memdb.Name,
		)
	}
	if err != nil {
		return false, fmt.Errorf("couldn't create db manager at %s: %w", m.rootConfig.DBPath, err)
	}
	defer func() {
		if err := dbManager.Close(); err != nil {
			m.log.Error("error closing db manager: %s", err)
		}
	}()

	currentDBBootstrapped, err := dbManager.Current().Database.Has(chains.BootstrappedKey)
	if err != nil {
		return false, fmt.Errorf("couldn't get if database version %s is bootstrapped: %w", version.CurrentDatabase, err)
	}
	if currentDBBootstrapped {
		return false, nil
	}
	_, exists := dbManager.Previous()
	return exists, nil
}

// Run two nodes simultaneously: one is a version before the database upgrade and the other after.
// The latter will bootstrap from the former.
// When the new node version is done bootstrapping, both nodes are stopped.
// Returns nil if the new node version successfully bootstrapped.
// Some configuration flags are modified before being passed into the 2 nodes.
func (m *migrationManager) runMigration() error {
	m.log.Info("starting database migration")
	m.nodeManager.lock.Lock()
	if m.nodeManager.hasShutdown {
		m.nodeManager.lock.Unlock()
		return nil
	}

	preDBUpgradeNode, err := m.nodeManager.preDBUpgradeNode()
	if err != nil {
		m.nodeManager.lock.Unlock()
		return fmt.Errorf("couldn't create pre-upgrade node during migration: %w", err)
	}
	m.log.Info("starting pre-database upgrade node")
	preDBUpgradeNodeExitCodeChan := preDBUpgradeNode.start()
	defer func() {
		if err := m.nodeManager.Stop(preDBUpgradeNode.path); err != nil {
			m.log.Error("%s", fmt.Errorf("error while stopping node at %s: %s", preDBUpgradeNode.path, err))
		}
	}()

	m.log.Info("starting latest node version")
	latestVersion, err := m.nodeManager.latestVersionNodeFetchOnly(m.rootConfig)
	if err != nil {
		m.nodeManager.lock.Unlock()
		return fmt.Errorf("couldn't create latest version during migration: %w", err)
	}
	latestVersionExitCodeChan := latestVersion.start()
	defer func() {
		if err := m.nodeManager.Stop(latestVersion.path); err != nil {
			m.log.Error("error while stopping latest version node: %s", err)
		}
	}()
	m.nodeManager.lock.Unlock()

	// Wait until one of the nodes finishes.
	// If the bootstrapping node finishes with an exit code other than
	// the one indicating it is done bootstrapping, error.
	select {
	case exitCode := <-preDBUpgradeNodeExitCodeChan:
		// If this node ended because the node manager shut down,
		// don't return an error
		m.nodeManager.lock.Lock()
		hasShutdown := m.nodeManager.hasShutdown
		m.nodeManager.lock.Unlock()
		if hasShutdown {
			return nil
		}
		return fmt.Errorf("previous version node stopped with exit code %d", exitCode)
	case exitCode := <-latestVersionExitCodeChan:
		if exitCode != constants.ExitCodeDoneMigrating {
			return fmt.Errorf("latest version died with exit code %d", exitCode)
		}

		return nil
	}
}
