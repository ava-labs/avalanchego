package main

import (
	"fmt"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
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
	avail, err := osDiskStat(storagePath)
	if err != nil {
		return err
	}
	used, err := dirSize(storagePath)
	if err != nil {
		return err
	}
	twox := used + used
	saftyBuf := (twox * 15) / 100
	required := twox + saftyBuf
	if avail < required {
		return fmt.Errorf("available space %d is less then required space %d for migration", avail, required)
	}
	if avail < constants.TwoGigabytes {
		m.log.Warn("at least 200GB of free disk space is recommended")
	}
	return nil
}

// Returns true if the database should be migrated from the previous database version.
// Should migrate if the previous database version exists and
// if the latest database version has not finished bootstrapping.
func (m *migrationManager) shouldMigrate() (bool, error) {
	if !m.rootConfig.DBEnabled {
		return false, nil
	}
	dbManager, err := manager.New(m.rootConfig.DBPath, logging.NoLog{}, node.DatabaseVersion, true)
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
		return false, fmt.Errorf("couldn't get if database version %s is bootstrapped: %w", node.DatabaseVersion, err)
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
