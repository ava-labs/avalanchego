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

func newMigrationManager(
	binaryManager *nodeManager,
	v *viper.Viper,
	nConfig node.Config,
	log logging.Logger,
) *migrationManager {
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

func (m *migrationManager) shouldMigrate() (bool, error) {
	if !m.nodeConfig.DBEnabled {
		return false, nil
	}
	dbManager, err := manager.New(m.nodeConfig.DBPath, logging.NoLog{}, versionconfig.CurrentDBVersion, true)
	if err != nil {
		return false, fmt.Errorf("couldn't create db manager at %s: %w", m.nodeConfig.DBPath, err)
	}
	defer func() {
		if err := dbManager.Shutdown(); err != nil {
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
// The latter will bootstrap from the former. Its staking port and HTTP port are 2
// greater than the staking/HTTP ports in [v].
// When the new node version is done bootstrapping, both nodes are stopped.
// Returns nil if the new node version successfully bootstrapped.
func (m *migrationManager) runMigration() error {
	m.log.Info("starting database migration")
	preDBUpgradeNode, err := m.binaryManager.preDBUpgradeNode(versionconfig.PreDBUpgradeNodeVersion.AsVersion(), m.v)
	if err != nil {
		return fmt.Errorf("couldn't create previous version node during migration: %w", err)
	}
	m.log.Info("starting node version %s", versionconfig.PreDBUpgradeNodeVersion.AsVersion())
	preDBUpgradeNodeExitCodeChan := preDBUpgradeNode.start()
	defer func() {
		if err := m.binaryManager.stop(preDBUpgradeNode.processID); err != nil {
			m.log.Error(err.Error())
		}
	}()

	m.log.Info("starting node version %s", versionconfig.NodeVersion.AsVersion())
	currentVersionNode, err := m.binaryManager.currentVersionNode(m.v, true, m.nodeConfig.NodeID)
	if err != nil {
		return fmt.Errorf("couldn't create current version during migration: %w", err)
	}
	currentVersionNodeExitCodeChan := currentVersionNode.start()
	defer func() {
		if err := currentVersionNode.stop(); err != nil {
			m.log.Error("error while stopping current version node: %s", err)
		}
	}()

	for {
		select {
		case exitCode := <-preDBUpgradeNodeExitCodeChan:
			return fmt.Errorf("previous version node stopped with exit code %d", exitCode)
		case exitCode := <-currentVersionNodeExitCodeChan:
			if exitCode != constants.ExitCodeDoneMigrating {
				return fmt.Errorf("current version died with exit code %d", exitCode)
			}
			return nil
		}
	}
}
