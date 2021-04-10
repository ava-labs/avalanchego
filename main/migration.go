package main

import (
	"fmt"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func shouldMigrate(nodeConfig node.Config, log logging.Logger) (bool, error) {
	if !nodeConfig.DBEnabled {
		return false, nil
	}
	dbManager, err := manager.New(nodeConfig.DBPath, logging.NoLog{}, config.DBVersion, true)
	if err != nil {
		return false, fmt.Errorf("couldn't create db manager at %s: %w", nodeConfig.DBPath, err)
	}
	defer func() {
		if err := dbManager.Close(); err != nil {
			log.Error("error closing db manager: %s", err)
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
