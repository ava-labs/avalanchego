// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// e2e implements the e2e tests.
package e2e

import (
	"flag"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	_ "github.com/ava-labs/subnet-evm/tests/e2e/ping"
	"github.com/ava-labs/subnet-evm/tests/e2e/runner"
	_ "github.com/ava-labs/subnet-evm/tests/e2e/solidity"
	"github.com/ava-labs/subnet-evm/tests/e2e/utils"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

func TestE2e(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "subnet-evm e2e test suites")
}

var (
	networkRunnerLogLevel string
	gRPCEp                string
	gRPCGatewayEp         string

	execPath  string
	pluginDir string
	logLevel  string

	vmGenesisPath string
	outputPath    string

	mode string
)

func init() {
	flag.StringVar(
		&networkRunnerLogLevel,
		"network-runner-log-level",
		"info",
		"gRPC server endpoint",
	)
	flag.StringVar(
		&gRPCEp,
		"network-runner-grpc-endpoint",
		"0.0.0.0:8080",
		"gRPC server endpoint",
	)
	flag.StringVar(
		&gRPCGatewayEp,
		"network-runner-grpc-gateway-endpoint",
		"0.0.0.0:8081",
		"gRPC gateway endpoint",
	)

	flag.StringVar(
		&execPath,
		"avalanchego-path",
		"",
		"avalanchego executable path",
	)
	flag.StringVar(
		&logLevel,
		"avalanchego-log-level",
		"INFO",
		"avalanchego log level",
	)
	flag.StringVar(
		&pluginDir,
		"avalanchego-plugin-dir",
		"",
		"avalanchego plugin directory",
	)
	flag.StringVar(
		&vmGenesisPath,
		"vm-genesis-path",
		"",
		"VM genesis file path",
	)
	flag.StringVar(
		&outputPath,
		"output-path",
		"",
		"output YAML path to write local cluster information",
	)

	flag.StringVar(
		&mode,
		"mode",
		"test",
		"'test' to shut down cluster after tests, 'run' to skip tests and only run without shutdown",
	)
}

func GetOutputPath() string {
	return outputPath
}

const vmName = "subnetevm"

var vmID ids.ID

func init() {
	// TODO: add "getVMID" util function in avalanchego and import from "avalanchego"
	b := make([]byte, 32)
	copy(b, []byte(vmName))
	var err error
	vmID, err = ids.ToID(b)
	if err != nil {
		panic(err)
	}
}

var _ = ginkgo.BeforeSuite(func() {
	gomega.Expect(mode).Should(gomega.Or(gomega.Equal("test"), gomega.Equal("run")))
	utils.SetOutputFile(outputPath)

	runner.InitializeRunner(execPath, gRPCEp, networkRunnerLogLevel)
})

var _ = ginkgo.AfterSuite(func() {
	runner.ShutdownCluster()
})

// var _ = ginkgo.Describe("[RPC server]", func() {
// 	ginkgo.It("can curl eth_blockNumber in every endpoint", func() {
// 		for _, ep := range subnetEVMRPCEps {
// 			matchedLine, err := tests.CURLPost(ep, `{
// 	"jsonrpc": "2.0",
// 	"method": "eth_blockNumber",
// 	"params": [],
// 	"id": 1
// }`, `{"jsonrpc":"2.0","id":1,"result":"0x0"}`)
// 			gomega.Expect(err).Should(gomega.BeNil())
// 			outf("{{cyan}}%q returned{{/}}: %s\n", ep, matchedLine)
// 		}
// 	})

// 	ginkgo.It("can TODO", func() {
// 		if mode != modeTest {
// 			ginkgo.Skip("mode is not 'test'; skipping...")
// 		}

// 		// TODO: e2e tests specific for subnet-evm
// 	})
// })

// Outputs to stdout.
//
// e.g.,
//   Out("{{green}}{{bold}}hi there %q{{/}}", "aa")
//   Out("{{magenta}}{{bold}}hi therea{{/}} {{cyan}}{{underline}}b{{/}}")
//
// ref.
// https://github.com/onsi/ginkgo/blob/v2.0.0/formatter/formatter.go#L52-L73
//
