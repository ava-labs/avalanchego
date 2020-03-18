// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"
	"path"
	"runtime"
	"runtime/pprof"

	"github.com/ava-labs/salticidae-go"

	"github.com/ava-labs/gecko/genesis"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/vms/avm"
	"github.com/ava-labs/gecko/vms/spchainvm"
	"github.com/ava-labs/gecko/vms/spdagvm"
)

func main() {
	if err != nil {
		fmt.Printf("Failed to parse arguments: %s\n", err)
	}

	// set up logging
	config.LoggingConfig.Directory = path.Join(config.LoggingConfig.Directory, "client")
	log, err := logging.New(config.LoggingConfig)
	if err != nil {
		fmt.Printf("Failed to start the logger: %s\n", err)
		return
	}

	defer log.Stop()

	// initialize state based on CLI args
	net.log = log
	crypto.EnableCrypto = config.EnableCrypto
	net.decided = make(chan ids.ID, config.MaxOutstandingTxs)

	if config.Key >= len(genesis.Keys) || config.Key < 0 {
		log.Fatal("Unknown key specified")
		return
	}

	// Init the network
	log.AssertNoError(net.Initialize())

	net.net.Start()
	defer net.net.Stop()

	// connect to the node
	serr := salticidae.NewError()
	remoteIP := salticidae.NewNetAddrFromIPPortString(config.RemoteIP.String(), true, &serr)
	if code := serr.GetCode(); code != 0 {
		log.Fatal("Sync error %s", salticidae.StrError(serr.GetCode()))
		return
	}

	net.conn = net.net.ConnectSync(remoteIP, true, &serr)
	if serr.GetCode() != 0 {
		log.Fatal("Sync error %s", salticidae.StrError(serr.GetCode()))
		return
	}

	// start a cpu profile
	file, gErr := os.Create("cpu_client.profile")
	log.AssertNoError(gErr)
	gErr = pprof.StartCPUProfile(file)
	log.AssertNoError(gErr)
	runtime.SetMutexProfileFraction(1)

	defer file.Close()
	defer pprof.StopCPUProfile()

	net.networkID = config.NetworkID

	// start the benchmark we want to run
	switch config.Chain {
	case spChain:
		net.benchmarkSPChain(genesis.VMGenesis(config.NetworkID, spchainvm.ID))
	case spDAG:
		net.benchmarkSPDAG(genesis.VMGenesis(config.NetworkID, spdagvm.ID))
	case avmDAG:
		net.benchmarkAVM(genesis.VMGenesis(config.NetworkID, avm.ID))
	default:
		log.Fatal("did not specify whether to test dag or chain. Exiting")
		return
	}

	// start processing network messages
	net.ec.Dispatch()
}
