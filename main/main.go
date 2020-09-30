// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"

	"github.com/ava-labs/avalanchego/nat"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	header = "" +
		`     _____               .__                       .__` + "\n" +
		`    /  _  \___  _______  |  | _____    ____   ____ |  |__   ____    ,_ o` + "\n" +
		`   /  /_\  \  \/ /\__  \ |  | \__  \  /    \_/ ___\|  |  \_/ __ \   / //\,` + "\n" +
		`  /    |    \   /  / __ \|  |__/ __ \|   |  \  \___|   Y  \  ___/    \>> |` + "\n" +
		`  \____|__  /\_/  (____  /____(____  /___|  /\___  >___|  /\___  >    \\` + "\n" +
		`          \/           \/          \/     \/     \/     \/     \/`
)

var (
	stakingPortName = fmt.Sprintf("%s-staking", constants.AppName)
	httpPortName    = fmt.Sprintf("%s-http", constants.AppName)
)

// main is the primary entry point to Avalanche.
func main() {
	// Err is set based on the CLI arguments
	if Err != nil {
		fmt.Printf("parsing parameters returned with error %s\n", Err)
		return
	}

	factory := logging.NewFactory(Config.LoggingConfig)
	defer factory.Close()

	log, err := factory.Make()
	if err != nil {
		fmt.Printf("starting logger failed with: %s\n", err)
		return
	}
	fmt.Println(header)

	defer func() { recover() }()

	defer log.Stop()
	defer log.StopOnPanic()
	defer Config.DB.Close()

	// Track if sybil control is enforced
	if !Config.EnableStaking && Config.EnableP2PTLS {
		log.Warn("Staking is disabled. Sybil control is not enforced.")
	}
	if !Config.EnableStaking && !Config.EnableP2PTLS {
		log.Warn("Staking and p2p encryption are disabled. Packet spoofing is possible.")
	}

	// Check if transaction signatures should be checked
	if !Config.EnableCrypto {
		log.Warn("transaction signatures are not being checked")
	}
	crypto.EnableCrypto = Config.EnableCrypto

	if err := Config.ConsensusParams.Valid(); err != nil {
		log.Fatal("consensus parameters are invalid: %s", err)
		return
	}

	// Track if assertions should be executed
	if Config.LoggingConfig.Assertions {
		log.Debug("assertions are enabled. This may slow down execution")
	}

	mapper := nat.NewPortMapper(log, Config.Nat)
	defer mapper.UnmapAllPorts()

	// Open staking port
	mapper.Map("TCP", Config.StakingIP.Port, Config.StakingIP.Port, stakingPortName)

	// Open the HTTP port iff the HTTP server is not listening on localhost
	if Config.HTTPHost != "127.0.0.1" && Config.HTTPHost != "localhost" {
		mapper.Map("TCP", Config.HTTPPort, Config.HTTPPort, httpPortName)
	}

	log.Debug("initializing node state")
	node := node.Node{}
	if err := node.Initialize(&Config, log, factory); err != nil {
		log.Fatal("error initializing node state: %s", err)
		return
	}

	defer node.Shutdown()

	log.Debug("dispatching node handlers")
	err = node.Dispatch()
	log.Debug("node dispatching returned with %s", err)
}
