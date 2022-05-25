// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// e2e implements the e2e tests.
package e2e_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	runner_sdk "github.com/ava-labs/avalanche-network-runner-sdk"
	"github.com/ava-labs/avalanchego/ids"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/formatter"
	"github.com/onsi/gomega"
	"sigs.k8s.io/yaml"
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

const (
	modeTest = "test"
	modeRun  = "run"
)

var (
	cli             runner_sdk.Client
	subnetEVMRPCEps []string
)

var _ = ginkgo.BeforeSuite(func() {
	gomega.Expect(mode).Should(gomega.Or(gomega.Equal("test"), gomega.Equal("run")))

	var err error
	cli, err = runner_sdk.New(runner_sdk.Config{
		LogLevel:    networkRunnerLogLevel,
		Endpoint:    gRPCEp,
		DialTimeout: 10 * time.Second,
	})
	gomega.Expect(err).Should(gomega.BeNil())

	ginkgo.By("calling start API via network runner", func() {
		outf("{{green}}sending 'start' with binary path:{{/}} %q\n", execPath)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		resp, err := cli.Start(
			ctx,
			execPath,
			runner_sdk.WithLogLevel(logLevel),
			runner_sdk.WithPluginDir(pluginDir),
			runner_sdk.WithCustomVMs(map[string]string{
				vmName: vmGenesisPath,
			}))
		cancel()
		gomega.Expect(err).Should(gomega.BeNil())
		outf("{{green}}successfully started:{{/}} %+v\n", resp.ClusterInfo.NodeNames)
	})

	// TODO: network runner health should imply custom VM healthiness
	// or provide a separate API for custom VM healthiness
	// "start" is async, so wait some time for cluster health
	outf("\n{{magenta}}sleeping before checking custom VM status...{{/}}\n")
	time.Sleep(2 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	_, err = cli.Health(ctx)
	cancel()
	gomega.Expect(err).Should(gomega.BeNil())

	subnetEVMRPCEps = make([]string, 0)
	blockchainID, logsDir := "", ""

	// wait up to 5-minute for custom VM installation
	outf("\n{{magenta}}waiting for all custom VMs to report healthy...{{/}}\n")
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
done:
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			break done
		case <-time.After(5 * time.Second):
		}

		outf("{{magenta}}checking custom VM status{{/}}\n")
		cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Minute)
		resp, err := cli.Status(cctx)
		ccancel()
		gomega.Expect(err).Should(gomega.BeNil())

		// all logs are stored under root data dir
		logsDir = resp.GetClusterInfo().GetRootDataDir()

		if v, ok := resp.ClusterInfo.CustomVms[vmID.String()]; ok {
			blockchainID = v.BlockchainId
			outf("{{blue}}subnet-evm is ready:{{/}} %+v\n", v)
			break done
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

	for _, u := range uris {
		rpcEP := fmt.Sprintf("%s/ext/bc/%s/rpc", u, blockchainID)
		subnetEVMRPCEps = append(subnetEVMRPCEps, rpcEP)
		outf("{{blue}}avalanche subnet-evm RPC:{{/}} %q\n", rpcEP)
	}

	pid := os.Getpid()
	outf("{{blue}}{{bold}}writing output %q with PID %d{{/}}\n", outputPath, pid)
	ci := clusterInfo{
		URIs:     uris,
		Endpoint: fmt.Sprintf("/ext/bc/%s", blockchainID),
		PID:      pid,
		LogsDir:  logsDir,
	}
	gomega.Expect(ci.Save(outputPath)).Should(gomega.BeNil())

	b, err := os.ReadFile(outputPath)
	gomega.Expect(err).Should(gomega.BeNil())
	outf("\n{{blue}}$ cat %s:{{/}}\n%s\n", outputPath, string(b))
})

var _ = ginkgo.AfterSuite(func() {
	switch mode {
	case modeTest:
		outf("{{red}}shutting down cluster{{/}}\n")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		_, err := cli.Stop(ctx)
		cancel()
		gomega.Expect(err).Should(gomega.BeNil())

	case modeRun:
		outf("{{red}}skipping shutting down cluster{{/}}\n")
	}

	outf("{{red}}shutting down client{{/}}\n")
	gomega.Expect(cli.Close()).Should(gomega.BeNil())
})

var _ = ginkgo.Describe("[basic]", func() {
	ginkgo.It("can TODO", func() {
		if mode != modeTest {
			ginkgo.Skip("mode is not 'test'; skipping...")
		}

		// TODO: e2e tests specific for subnet-evm
	})
})

// Outputs to stdout.
//
// e.g.,
//   Out("{{green}}{{bold}}hi there %q{{/}}", "aa")
//   Out("{{magenta}}{{bold}}hi therea{{/}} {{cyan}}{{underline}}b{{/}}")
//
// ref.
// https://github.com/onsi/ginkgo/blob/v2.0.0/formatter/formatter.go#L52-L73
//
func outf(format string, args ...interface{}) {
	s := formatter.F(format, args...)
	fmt.Fprint(formatter.ColorableStdOut, s)
}

// clusterInfo represents the local cluster information.
type clusterInfo struct {
	URIs     []string `json:"uris"`
	Endpoint string   `json:"endpoint"`
	PID      int      `json:"pid"`
	LogsDir  string   `json:"logsDir"`
}

const fsModeWrite = 0o600

func (ci clusterInfo) Save(p string) error {
	ob, err := yaml.Marshal(ci)
	if err != nil {
		return err
	}
	return os.WriteFile(p, ob, fsModeWrite)
}
