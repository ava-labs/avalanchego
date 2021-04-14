package main

import (
	"fmt"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/spf13/viper"
)

type migrationManager struct {
	binaryManager *nodeProcessManager
	nodeConfig    node.Config
	log           logging.Logger
	viperConfig   *viper.Viper
}

func newMigrationManager(binaryManager *nodeProcessManager, v *viper.Viper, nConfig node.Config, log logging.Logger) *migrationManager {
	return &migrationManager{
		binaryManager: binaryManager,
		nodeConfig:    nConfig,
		log:           log,
		viperConfig:   v,
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
	dbManager, err := manager.New(m.nodeConfig.DBPath, logging.NoLog{}, config.DBVersion, true)
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
		return false, fmt.Errorf("couldn't get if database version %s is bootstrapped: %w", config.DBVersion, err)
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
	prevVersionNode, err := m.binaryManager.previousVersionNode(node.PreviousVersion.AsVersion(), m.viperConfig)
	if err != nil {
		return fmt.Errorf("couldn't create previous version node during migration: %w", err)
	}
	prevVersionNodeExitCodeChan := prevVersionNode.start()
	defer func() {
		if err := m.binaryManager.stop(prevVersionNode.processID); err != nil {
			m.log.Error(err.Error())
		}
	}()

	currentVersionNode, err := m.binaryManager.currentVersionNode(m.viperConfig, true, m.nodeConfig.NodeID)
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
		case exitCode := <-prevVersionNodeExitCodeChan:
			return fmt.Errorf("previous version node stopped with exit code %d", exitCode)
		case exitCode := <-currentVersionNodeExitCodeChan:
			if exitCode != constants.ExitCodeDoneMigrating {
				return fmt.Errorf("current version died with exit code %d", exitCode)
			}
			return nil
		}
	}
}
