// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"path"

	"github.com/ava-labs/gecko/nat"
	"github.com/ava-labs/gecko/node"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/logging"
)

// main is the primary entry point to Ava. This can either create a CLI to an
//     existing node or create a new node.
func main() {
	// Err is set based on the CLI arguments
	if Err != nil {
		fmt.Printf("parsing parameters returned with error %s\n", Err)
		return
	}

	config := Config.LoggingConfig
	config.Directory = path.Join(config.Directory, "node")
	factory := logging.NewFactory(config)
	defer factory.Close()

	log, err := factory.Make()
	if err != nil {
		fmt.Printf("starting logger failed with: %s\n", err)
		return
	}
	fmt.Println(gecko)

	defer func() { recover() }()

	defer log.Stop()
	defer log.StopOnPanic()
	defer Config.DB.Close()

	if Config.StakingIP.IsZero() {
		log.Warn("NAT traversal has failed. If this node becomes a staker, it may lose its reward due to being unreachable.")
	}

	// Track if sybil control is enforced
	if !Config.EnableStaking {
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
		log.Warn("assertions are enabled. This may slow down execution")
	}

	mapper := nat.NewDefaultMapper(log, Config.Nat, nat.TCP, "gecko")
	defer mapper.UnmapAllPorts()

	mapper.MapPort(Config.StakingIP.Port, Config.StakingIP.Port)
	mapper.MapPort(Config.HTTPPort, Config.HTTPPort)

	log.Debug("initializing node state")
	// MainNode is a global variable in the node.go file
	if err := node.MainNode.Initialize(&Config, log, factory); err != nil {
		log.Fatal("error initializing node state: %s", err)
		return
	}

	log.Debug("Starting servers")
	if err := node.MainNode.StartConsensusServer(); err != nil {
		log.Fatal("problem starting servers: %s", err)
		return
	}

	defer node.MainNode.Shutdown()

	log.Debug("Dispatching node handlers")
	node.MainNode.Dispatch()
}
