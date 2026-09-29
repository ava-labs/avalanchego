// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/antithesis"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

const (
	baseImageName = "antithesis-avalanchego"
	nodeCount     = 5

	// We schedule the latest upgrade to activate partway through a run so that
	// the transition across a network upgrade is exercised as part of the test.
	minActivationOffset = 30 * time.Second
	maxActivationOffset = 500 * time.Second
)

// Creates docker-compose.yml, guest.sh, and the associated volumes in the
// target path.
func main() {
	log := tests.NewDefaultLogger("")
	network, activationTime, err := newNetwork()
	if err != nil {
		log.Fatal("failed to configure network",
			zap.Error(err),
		)
		os.Exit(1)
	}
	if err := antithesis.GenerateComposeConfig(network, baseImageName); err != nil {
		log.Fatal("failed to generate compose config",
			zap.Error(err),
		)
		os.Exit(1)
	}
	if err := antithesis.WriteGuestScript(guestScript(activationTime)); err != nil {
		log.Fatal("failed to write guest script",
			zap.Error(err),
		)
		os.Exit(1)
	}
}

// newNetwork returns a network with genesis time of [time.Now], all previous
// upgrades initially active, and the latest upgrade scheduled to happen
// [maxActivationOffset] after [time.Now]. A custom network is required because
// nodes refuse to override the local network's upgrade schedule.
//
// The timestamp of the latest upgrade is returned.
func newNetwork() (*tmpnet.Network, time.Time, error) {
	network := &tmpnet.Network{
		Owner: baseImageName,
		Nodes: tmpnet.NewNodesOrPanic(nodeCount),
	}
	genesis, err := network.DefaultGenesis()
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("creating genesis: %w", err)
	}
	network.Genesis = genesis

	upgrades := tmpnet.UpgradeConfig(maxActivationOffset)
	network.DefaultFlags, err = tmpnet.UpgradeFlags(upgrades)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("getting upgrade flags: %w", err)
	}
	return network, upgrades.HeliconTime, nil // Must be updated for each network upgrade
}

// guestScript returns a bash script that sets the system clock to a random
// offset in [minActivationOffset, maxActivationOffset] before activationTime so
// that the latest upgrade activates that random duration after the network
// starts.
func guestScript(activationTime time.Time) string {
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

# Latest upgrade activation time, in seconds since the Unix epoch. Baked in when
# the antithesis config image was generated.
activation_epoch=%d
# Bounds (in seconds) on how long the network should be up before the latest
# upgrade activates.
min_offset=%d
max_offset=%d

# Draw a random offset in [min_offset, max_offset].
rand=$(od -An -N4 -tu4 < /dev/urandom | tr -d '[:space:]')
offset=$(( min_offset + rand %% (max_offset - min_offset + 1) ))

start_epoch=$(( activation_epoch - offset ))
start_time=$(date -u -d "@${start_epoch}" +"%%Y-%%m-%%d %%H:%%M:%%S")
echo "Setting system clock to ${start_time} UTC; Helicon activates in ${offset}s"
timedatectl set-time "${start_time}"
`,
		activationTime.Unix(),
		int(minActivationOffset.Seconds()),
		int(maxActivationOffset.Seconds()),
	)
}
