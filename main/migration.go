package main

import (
	"fmt"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/spf13/viper"
	"strings"
)

type MigrationManager struct {
	nodeConfig    *node.Config
	dbVersion     version.Version
	prevDBVersion version.Version
	binaryManager *BinaryManager
	viper         *viper.Viper
}

func NewMigrationManager(binaryManager *BinaryManager, nodeConfig *node.Config, viper *viper.Viper, dbVersion version.Version, prevDBVersion version.Version) *MigrationManager {
	return &MigrationManager{
		nodeConfig:    nodeConfig,
		dbVersion:     dbVersion,
		prevDBVersion: prevDBVersion,
		binaryManager: binaryManager,
		viper:         viper,
	}
}

// ResolveMigration decides based on the database whether to run normally or in fetch mode
func (m *MigrationManager) ResolveMigration() error {
	var needDBUpgrade bool

	if m.nodeConfig.DBEnabled {
		dbManager, err := manager.New(m.nodeConfig.DBPath, logging.NoLog{}, m.dbVersion, true)
		if err != nil {
			fmt.Printf("couldn't create db manager at %s: %s\n", m.nodeConfig.DBPath, err)
			return err
		}
		currentDBBootstrapped, err := dbManager.CurrentDBBootstrapped()
		if err != nil {
			fmt.Printf("couldn't get if database is bootstrapped: %s", err)
			return err
		}
		if !currentDBBootstrapped {
			if _, exists := dbManager.Previous(); exists {
				needDBUpgrade = true
			}
		}
		if err = dbManager.Close(); err != nil {
			fmt.Printf("couldn't close database manager: %s", err)
			return err
		}
	}
	fmt.Printf("upgrading database version: %v\n", needDBUpgrade)
	return m.setupApp(needDBUpgrade)
}

func (m *MigrationManager) setupApp(upgrade bool) error {
	var cmdArgs []string

	// setting up the current version
	for k, v := range m.viper.AllSettings() { // Pass args
		cmdArgs = append(cmdArgs, fmt.Sprintf("--%s=%v", k, v))
	}

	// todo hook versions dynamically
	m.binaryManager.currVsApp = &application{
		path:    m.binaryManager.rootPath + "/build/avalanchego-" + "v1.3.2" + "/avalanchego-inner",
		errChan: make(chan error),
		setup:   true,
		args:    cmdArgs,
	}

	if upgrade {
		// setting up the previous version
		var prevCmdArgs []string
		for k, v := range m.viper.AllSettings() { // Pass args
			if strings.Contains(k, "fetch") {
				continue
			}
			prevCmdArgs = append(prevCmdArgs, fmt.Sprintf("--%s=%v", k, v))
		}

		// todo hook versions dynamically
		m.binaryManager.prevVsApp = &application{
			path:    m.binaryManager.rootPath + "/build/avalanchego-" + "v1.3.1" + "/avalanchego-inner",
			errChan: make(chan error),
			setup:   true,
			args:    prevCmdArgs,
		}
	}

	return nil
}
