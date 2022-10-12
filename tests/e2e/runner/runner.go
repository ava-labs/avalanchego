package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ava-labs/avalanche-network-runner/client"
	"github.com/ava-labs/avalanche-network-runner/rpcpb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/subnet-evm/tests/e2e/utils"
	"github.com/onsi/ginkgo/v2/formatter"

	"sigs.k8s.io/yaml"
)

var (
	execPath string

	cli client.Client
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

func GetClient() client.Client {
	return cli
}

func InitializeRunner(execPath_ string, grpcEp string, networkRunnerLogLevel string) error {
	execPath = execPath_

	// Create the logger
	logLevel, err := logging.ToLevel(networkRunnerLogLevel)
	if err != nil {
		return err
	}

	logFactory := logging.NewFactory(logging.Config{
		DisplayLevel: logLevel,
		LogLevel:     logging.Off, // Disable writing logs to files in favor of only writing logs to display
	})
	log, err := logFactory.Make("main")
	if err != nil {
		return err
	}

	cli, err = client.New(client.Config{
		Endpoint:    grpcEp,
		DialTimeout: 10 * time.Second,
	}, log)
	return err
}

func startRunner(vmName string, genesisPath string, pluginDir string) error {
	fmt.Println("calling start API via network runner")
	outf("{{green}}sending 'start' with binary path:{{/}} %q\n", execPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	resp, err := cli.Start(
		ctx,
		execPath,
		client.WithPluginDir(pluginDir),
		client.WithBlockchainSpecs([]*rpcpb.BlockchainSpec{
			{
				VmName:  vmName,
				Genesis: genesisPath,
			},
		}),
	)
	cancel()
	if err != nil {
		return err
	}
	outf("{{green}}successfully started:{{/}} %+v\n", resp.ClusterInfo.NodeNames)
	return nil
}

func WaitForCustomVm(vmId ids.ID) (string, string, int, error) {
	blockchainID, logsDir := "", ""
	pid := 0

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
		resp, err := cli.Health(cctx)
		ccancel()
		if err != nil {
			cancel()
			return "", "", 0, err
		}

		if !resp.ClusterInfo.Healthy {
			continue
		}

		if !resp.ClusterInfo.CustomChainsHealthy {
			continue
		}

		// all logs are stored under root data dir
		logsDir = resp.GetClusterInfo().GetRootDataDir()

		// ANR server pid
		pid = int(resp.GetClusterInfo().GetPid())

		for chainID, chainInfo := range resp.ClusterInfo.CustomChains {
			if chainInfo.VmId == vmId.String() {
				blockchainID = chainID
				outf("{{blue}}subnet-evm is ready:{{/}} %+v\n", chainInfo)
				break done
			}
		}
	}
	err := ctx.Err()
	if err != nil {
		cancel()
		return "", "", 0, err
	}
	cancel()

	if blockchainID == "" {
		return "", "", 0, errors.New("BlockchainId not found")
	}
	if logsDir == "" {
		return "", "", 0, errors.New("logsDir not found")
	}
	if pid == 0 {
		return "", "", pid, errors.New("pid not found")
	}
	return blockchainID, logsDir, pid, nil
}

func SaveClusterInfo(blockchainId string, logsDir string, pid int) (clusterInfo, error) {
	cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Minute)
	uris, err := cli.URIs(cctx)
	ccancel()
	if err != nil {
		return clusterInfo{}, err
	}
	outf("{{blue}}avalanche HTTP RPCs URIs:{{/}} %q\n", uris)

	subnetEVMRPCEps := make([]string, 0)
	for _, u := range uris {
		rpcEP := fmt.Sprintf("%s/ext/bc/%s/rpc", u, blockchainId)
		subnetEVMRPCEps = append(subnetEVMRPCEps, rpcEP)
		outf("{{blue}}avalanche subnet-evm RPC:{{/}} %q\n", rpcEP)
	}

	ci := clusterInfo{
		URIs:     uris,
		Endpoint: fmt.Sprintf("/ext/bc/%s", blockchainId),
		PID:      pid,
		LogsDir:  logsDir,
	}
	err = ci.Save(utils.GetOutputPath())
	if err != nil {
		return clusterInfo{}, err
	}
	return ci, nil
}

func StartNetwork(vmId ids.ID, vmName string, genesisPath string, pluginDir string) (clusterInfo, error) {
	fmt.Println("Starting network")
	startRunner(vmName, genesisPath, pluginDir)

	blockchainId, logsDir, pid, err := WaitForCustomVm(vmId)
	if err != nil {
		return clusterInfo{}, err
	}
	fmt.Println("Got custom vm")

	return SaveClusterInfo(blockchainId, logsDir, pid)
}

func StopNetwork() error {
	outf("{{red}}shutting down network{{/}}\n")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	_, err := cli.Stop(ctx)
	cancel()
	return err
}

func ShutdownClient() error {
	outf("{{red}}shutting down client{{/}}\n")
	return cli.Close()
}

func outf(format string, args ...interface{}) {
	s := formatter.F(format, args...)
	fmt.Fprint(formatter.ColorableStdOut, s)
}

func IsRunnerUp() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_, err := cli.Health(ctx)
	cancel()
	return err == nil
}
