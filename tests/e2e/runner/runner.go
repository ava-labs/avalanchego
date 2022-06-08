package runner

import (
	"context"
	"fmt"
	"os"
	"time"

	runner_sdk "github.com/ava-labs/avalanche-network-runner-sdk"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/subnet-evm/tests/e2e/utils"

	// "github.com/influxdata/influxdb/client"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/formatter"
	"github.com/onsi/gomega"
	"sigs.k8s.io/yaml"
)

var (
	// networkRunnerLogLevel string
	// gRPCEp                string
	// gRPCGatewayEp         string

	execPath string
	// pluginDir string
	logLevel string

	// outputPath string

	// mode string
	cli runner_sdk.Client
)

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

func GetClient() runner_sdk.Client {
	return cli
}

func InitializeRunner(execPath_ string, grpcEp string, networkRunnerLogLevel string) {
	execPath = execPath_

	var err error
	cli, err = runner_sdk.New(runner_sdk.Config{
		LogLevel:    networkRunnerLogLevel,
		Endpoint:    grpcEp,
		DialTimeout: 10 * time.Second,
	})
	gomega.Expect(err).Should(gomega.BeNil())
}

func startRunner(vmName string, genesisPath string, pluginDir string) {
	fmt.Println("Args", vmName, genesisPath, pluginDir)
	ginkgo.By("calling start API via network runner", func() {
		outf("{{green}}sending 'start' with binary path:{{/}} %q\n", execPath)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		resp, err := cli.Start(
			ctx,
			execPath,
			runner_sdk.WithLogLevel(logLevel),
			runner_sdk.WithPluginDir(pluginDir),
			runner_sdk.WithCustomVMs(map[string]string{
				vmName: genesisPath,
			}))
		cancel()
		gomega.Expect(err).Should(gomega.BeNil())
		outf("{{green}}successfully started:{{/}} %+v\n", resp.ClusterInfo.NodeNames)
	})
}

func checkRunnerHealth() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	_, err := cli.Health(ctx)
	cancel()
	gomega.Expect(err).Should(gomega.BeNil())
}

func waitForCustomVm(vmId ids.ID) (string, string) {
	blockchainID, logsDir := "", ""

	// wait up to 5-minute for custom VM installation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
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

		if v, ok := resp.ClusterInfo.CustomVms[vmId.String()]; ok {
			blockchainID = v.BlockchainId
			outf("{{blue}}subnet-evm is ready:{{/}} %+v\n", v)
			break done
		}
	}
	gomega.Expect(ctx.Err()).Should(gomega.BeNil())
	cancel()

	gomega.Expect(blockchainID).Should(gomega.Not(gomega.BeEmpty()))
	gomega.Expect(logsDir).Should(gomega.Not(gomega.BeEmpty()))
	return blockchainID, logsDir
}

func getClusterInfo(blockchainId string, logsDir string) clusterInfo {
	cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Minute)
	uris, err := cli.URIs(cctx)
	ccancel()
	gomega.Expect(err).Should(gomega.BeNil())
	outf("{{blue}}avalanche HTTP RPCs URIs:{{/}} %q\n", uris)

	subnetEVMRPCEps := make([]string, 0)
	for _, u := range uris {
		rpcEP := fmt.Sprintf("%s/ext/bc/%s/rpc", u, blockchainId)
		subnetEVMRPCEps = append(subnetEVMRPCEps, rpcEP)
		outf("{{blue}}avalanche subnet-evm RPC:{{/}} %q\n", rpcEP)
	}

	pid := os.Getpid()
	// outf("{{blue}}{{bold}}writing output %q with PID %d{{/}}\n", outputPath, pid)
	ci := clusterInfo{
		URIs:     uris,
		Endpoint: fmt.Sprintf("/ext/bc/%s", blockchainId),
		PID:      pid,
		LogsDir:  logsDir,
	}
	gomega.Expect(ci.Save(utils.GetOutputPath())).Should(gomega.BeNil())
	return ci
}

func StartNetwork(vmId ids.ID, vmName string, genesisPath string, pluginDir string) clusterInfo {
	fmt.Println("Starting network")
	startRunner(vmName, genesisPath, pluginDir)

	// TODO: network runner health should imply custom VM healthiness
	// or provide a separate API for custom VM healthiness
	// "start" is async, so wait some time for cluster health
	fmt.Println("About to sleep")
	time.Sleep(2 * time.Minute)
	checkRunnerHealth()

	fmt.Println("Health checked")
	blockchainId, logsDir := waitForCustomVm(vmId)
	fmt.Println("Got custom vm")

	cluster := getClusterInfo(blockchainId, logsDir)

	// b, err := os.ReadFile(outputPath)
	// gomega.Expect(err).Should(gomega.BeNil())
	// outf("\n{{blue}}$ cat %s:{{/}}\n%s\n", outputPath, string(b))
	return cluster
}

func ShutdownCluster() {
	outf("{{red}}shutting down cluster{{/}}\n")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	_, err := cli.Stop(ctx)
	cancel()
	gomega.Expect(err).Should(gomega.BeNil())

	outf("{{red}}shutting down client{{/}}\n")
	gomega.Expect(cli.Close()).Should(gomega.BeNil())
}

func outf(format string, args ...interface{}) {
	s := formatter.F(format, args...)
	fmt.Fprint(formatter.ColorableStdOut, s)
}
