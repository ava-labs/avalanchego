// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// load implements the load tests.
package load_test

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"testing"
	"time"

	runner_sdk "github.com/ava-labs/avalanche-network-runner/client"
	"github.com/ava-labs/avalanche-network-runner/rpcpb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	log "github.com/inconshreveable/log15"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/formatter"
	"github.com/onsi/gomega"
)

func TestLoad(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "timestampvm load test suites")
}

var (
	requestTimeout time.Duration

	networkRunnerLogLevel string
	gRPCEp                string
	gRPCGatewayEp         string

	execPath  string
	pluginDir string

	vmGenesisPath    string
	vmConfigPath     string
	subnetConfigPath string

	// Comma separated list of client URIs
	// If the length is non-zero, this will skip using the network runner to start and stop a network.
	commaSeparatedClientURIs string
	// Specifies the full timestampvm client URIs to use for load test.
	// Populated in BeforeSuite
	clientURIs []string

	terminalHeight uint64

	blockchainID string
	logsDir      string
)

func init() {
	flag.DurationVar(
		&requestTimeout,
		"request-timeout",
		120*time.Second,
		"timeout for transaction issuance and confirmation",
	)

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
		&vmConfigPath,
		"vm-config-path",
		"",
		"VM configfile path",
	)

	flag.StringVar(
		&subnetConfigPath,
		"subnet-config-path",
		"",
		"Subnet configfile path",
	)

	flag.Uint64Var(
		&terminalHeight,
		"terminal-height",
		1_000_000,
		"height to quit at",
	)

	flag.StringVar(
		&commaSeparatedClientURIs,
		"client-uris",
		"",
		"Specifies a comma separated list of full timestampvm client URIs to use in place of orchestrating a network. (Ex. 127.0.0.1:9650/ext/bc/q2aTwKuyzgs8pynF7UXBZCU7DejbZbZ6EUyHr3JQzYgwNPUPi/rpc,127.0.0.1:9652/ext/bc/q2aTwKuyzgs8pynF7UXBZCU7DejbZbZ6EUyHr3JQzYgwNPUPi/rpc",
	)
}

const vmName = "timestamp"

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

var (
	cli               runner_sdk.Client
	timestampvmRPCEps []string
)

var _ = ginkgo.BeforeSuite(func() {
	if len(commaSeparatedClientURIs) != 0 {
		clientURIs = strings.Split(commaSeparatedClientURIs, ",")
		outf("{{green}}creating %d clients from manually specified URIs:{{/}}\n", len(clientURIs))
		return
	}

	logLevel, err := logging.ToLevel(networkRunnerLogLevel)
	gomega.Expect(err).Should(gomega.BeNil())
	logFactory := logging.NewFactory(logging.Config{
		DisplayLevel: logLevel,
		LogLevel:     logLevel,
	})
	log, err := logFactory.Make("main")
	gomega.Expect(err).Should(gomega.BeNil())

	cli, err = runner_sdk.New(runner_sdk.Config{
		Endpoint:    gRPCEp,
		DialTimeout: 10 * time.Second,
	}, log)
	gomega.Expect(err).Should(gomega.BeNil())

	ginkgo.By("calling start API via network runner", func() {
		outf("{{green}}sending 'start' with binary path:{{/}} %q (%q)\n", execPath, vmID)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		resp, err := cli.Start(
			ctx,
			execPath,
			runner_sdk.WithPluginDir(pluginDir),
			runner_sdk.WithBlockchainSpecs(
				[]*rpcpb.BlockchainSpec{
					{
						VmName:      vmName,
						Genesis:     vmGenesisPath,
						ChainConfig: vmConfigPath,
						SubnetSpec: &rpcpb.SubnetSpec{
							SubnetConfig: subnetConfigPath,
						},
					},
				},
			),
			// Disable all rate limiting
			runner_sdk.WithGlobalNodeConfig(`{
				"log-level":"warn",
				"proposervm-use-current-height":true,
				"throttler-inbound-validator-alloc-size":"107374182",
				"throttler-inbound-node-max-processing-msgs":"100000",
				"throttler-inbound-bandwidth-refill-rate":"1073741824",
				"throttler-inbound-bandwidth-max-burst-size":"1073741824",
				"throttler-inbound-cpu-validator-alloc":"100000",
				"throttler-inbound-disk-validator-alloc":"10737418240000",
				"throttler-outbound-validator-alloc-size":"107374182"
			}`),
		)
		cancel()
		gomega.Expect(err).Should(gomega.BeNil())
		outf("{{green}}successfully started:{{/}} %+v\n", resp.ClusterInfo.NodeNames)
	})

	// TODO: network runner health should imply custom VM healthiness
	// or provide a separate API for custom VM healthiness
	// "start" is async, so wait some time for cluster health
	outf("\n{{magenta}}waiting for all vms to report healthy...{{/}}: %s\n", vmID)
	for {
		healthRes, err := cli.Health(context.Background())
		if err != nil || !healthRes.ClusterInfo.Healthy {
			time.Sleep(1 * time.Second)
			continue
		}
		break
	}

	timestampvmRPCEps = make([]string, 0)

	// wait up to 5-minute for custom VM installation
	outf("\n{{magenta}}waiting for all custom VMs to report healthy...{{/}}\n")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
done:
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			break done
		case <-time.After(5 * time.Second):
		}

		cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Minute)
		resp, err := cli.Status(cctx)
		ccancel()
		gomega.Expect(err).Should(gomega.BeNil())

		// all logs are stored under root data dir
		logsDir = resp.GetClusterInfo().GetRootDataDir()

		for _, v := range resp.ClusterInfo.CustomChains {
			if v.VmId == vmID.String() {
				blockchainID = v.ChainId
				outf("{{blue}}timestampvm is ready:{{/}} %+v\n", v)
				break done
			}
		}
	}
	gomega.Expect(ctx.Err()).Should(gomega.BeNil())
	cancel()

	gomega.Expect(blockchainID).Should(gomega.Not(gomega.BeEmpty()))
	gomega.Expect(logsDir).Should(gomega.Not(gomega.BeEmpty()))

	cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Minute)
	uris, err := cli.URIs(cctx)
	ccancel()
	gomega.Expect(err).Should(gomega.BeNil())
	outf("{{blue}}avalanche HTTP RPCs URIs:{{/}} %q\n", uris)

	clientURIs = uris

	outf("{{magenta}}logs dir:{{/}} %s\n", logsDir)
})

var _ = ginkgo.AfterSuite(func() {
	// If clientURIs were manually specified, skip killing the network.
	if len(commaSeparatedClientURIs) != 0 {
		return
	}
	outf("{{red}}shutting down cluster{{/}}\n")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	_, err := cli.Stop(ctx)
	cancel()
	gomega.Expect(err).Should(gomega.BeNil())
	log.Warn("cluster shutdown result", "err", err)

	outf("{{red}}shutting down client{{/}}\n")
	err = cli.Close()
	gomega.Expect(err).Should(gomega.BeNil())
	log.Warn("client shutdown result", "err", err)
})

// Tests only assumes that [clientURIs] has been populated by BeforeSuite
var _ = ginkgo.Describe("[ProposeBlock]", func() {
	ginkgo.It("load test", func() {
		workers := newLoadWorkers(clientURIs, blockchainID)

		err := RunLoadTest(context.Background(), workers, terminalHeight, 2*time.Minute)
		gomega.Ω(err).Should(gomega.BeNil())
		log.Info("Load test completed successfully")
	})
})

// Outputs to stdout.
//
// e.g.,
//
//	Out("{{green}}{{bold}}hi there %q{{/}}", "aa")
//	Out("{{magenta}}{{bold}}hi therea{{/}} {{cyan}}{{underline}}b{{/}}")
//
// ref.
// https://github.com/onsi/ginkgo/blob/v2.0.0/formatter/formatter.go#L52-L73
func outf(format string, args ...interface{}) {
	s := formatter.F(format, args...)
	fmt.Fprint(formatter.ColorableStdOut, s)
}
