package main

import (
	"fmt"

	"github.com/ava-labs/avalanchego/config/versionconfig"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/spf13/viper"
)

type migrationManager struct {
	binaryManager *nodeManager
	nodeConfig    node.Config
	log           logging.Logger
	v             *viper.Viper
}

func newMigrationManager(binaryManager *nodeManager, v *viper.Viper, nConfig node.Config, log logging.Logger) *migrationManager {
	return &migrationManager{
		binaryManager: binaryManager,
		nodeConfig:    nConfig,
		log:           log,
		v:             v,
	}
}

func (m *migrationManager) migrate() error {
	shouldMigrate, err := m.shouldMigrate()
	if err != nil {
		return err
	}
	if !shouldMigrate {
		return nil
	}
	return m.runMigration()
}

// Return true if the database should be migrated from the previous database version
// to the current one. We should migrate if we have not finished bootstrapping with the
// current database version and the previous database version exists.
func (m *migrationManager) shouldMigrate() (bool, error) {
	if !m.nodeConfig.DBEnabled {
		return false, nil
	}
	dbManager, err := manager.New(m.nodeConfig.DBPath, logging.NoLog{}, versionconfig.CurrentDBVersion, true)
	if err != nil {
		return false, fmt.Errorf("couldn't create db manager at %s: %w", m.nodeConfig.DBPath, err)
	}
	defer func() {
		if err := dbManager.Close(); err != nil {
			m.log.Error("error shutting down db manager: %s", err)
		}
	}()
	currentDBBootstrapped, err := dbManager.CurrentDBBootstrapped()
	if err != nil {
		return false, fmt.Errorf("couldn't get if database version %s is bootstrapped: %w", versionconfig.CurrentDBVersion, err)
	}
	if currentDBBootstrapped {
		return false, nil
	}
	_, exists := dbManager.Previous()
	return exists, nil
}

// Run two nodes at once: one is a version before the database upgrade and the other after.
// The latter will bootstrap from the former. The pre-upgrade node will have staking port and
// HTTP port 2 greater than the values given by the user.
// When the new node version is done bootstrapping, both nodes are stopped.
// Returns nil if the new node version successfully bootstrapped.
func (m *migrationManager) runMigration() error {
	m.log.Info("starting database migration")
	preDBUpgradeNode, err := m.binaryManager.preDBUpgradeNode(m.v)
	if err != nil {
		return fmt.Errorf("couldn't create pre-upgrade node during migration: %w", err)
	}
	m.log.Info("starting pre-database upgrade node")
	preDBUpgradeNodeExitCodeChan := preDBUpgradeNode.start()
	defer func() {
		if err := m.binaryManager.Stop(preDBUpgradeNode.processID); err != nil {
			m.log.Error(err.Error())
		}
	}()

	m.log.Info("starting latest node version")
	latestVersion, err := m.binaryManager.latestVersionNodeFetchOnly(m.v, m.nodeConfig)
	if err != nil {
		return fmt.Errorf("couldn't create latest version during migration: %w", err)
	}
	latestVersionExitCodeChan := latestVersion.start()
	defer func() {
		if err := m.binaryManager.Stop(latestVersion.processID); err != nil {
			m.log.Error("error while stopping latest version node: %s", err)
		}
	}()

	// Wait until one of the nodes finishes. If the bootstrapping node finishes with
	// an exit code other than the one indicating it is done bootstrapping, error.
	select {
	case exitCode := <-preDBUpgradeNodeExitCodeChan:
		return fmt.Errorf("previous version node stopped with exit code %d", exitCode)
	case exitCode := <-latestVersionExitCodeChan:
		if exitCode != constants.ExitCodeDoneMigrating {
			return fmt.Errorf("latest version died with exit code %d", exitCode)
		}
		return nil
	}
}
