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
	"github.com/ava-labs/gecko/vms/platformvm"
)

func main() {
	if err != nil {
		fmt.Printf("Failed to parse arguments: %s\n", err)
	}

	config.LoggingConfig.Directory = path.Join(config.LoggingConfig.Directory, "client")
	log, err := logging.New(config.LoggingConfig)
	if err != nil {
		fmt.Printf("Failed to start the logger: %s\n", err)
		return
	}

	defer log.Stop()

	net.log = log
	crypto.EnableCrypto = config.EnableCrypto
	net.decided = make(chan ids.ID, config.MaxOutstandingTxs)

	if config.Key >= len(genesis.Keys) || config.Key < 0 {
		log.Fatal("Unknown key specified")
		return
	}

	log.AssertNoError(net.Initialize())

	net.net.Start()
	defer net.net.Stop()

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

	file, gErr := os.Create("cpu_client.profile")
	log.AssertNoError(gErr)
	gErr = pprof.StartCPUProfile(file)
	log.AssertNoError(gErr)
	runtime.SetMutexProfileFraction(1)

	defer file.Close()
	defer pprof.StopCPUProfile()

	net.networkID = config.NetworkID

	platformGenesisBytes := genesis.Genesis(net.networkID)
	genesisState := &platformvm.Genesis{}
	log.AssertNoError(platformvm.Codec.Unmarshal(platformGenesisBytes, genesisState))
	log.AssertNoError(genesisState.Initialize())

	switch config.Chain {
	case spChain:
		net.benchmarkSPChain(genesisState)
	case spDAG:
		net.benchmarkSPDAG(genesisState)
	case avmDAG:
		net.benchmarkAVM(genesisState)
	default:
		log.Fatal("did not specify whether to test dag or chain. Exiting")
		return
	}

	net.ec.Dispatch()
}
