// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// runner uses "avalanche-network-runner" to set up a local network.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/ava-labs/avalanche-network-runner/api"
	"github.com/ava-labs/avalanche-network-runner/local"
	"github.com/ava-labs/avalanche-network-runner/network"
	"github.com/ava-labs/avalanche-network-runner/network/node"
	avago_api "github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	avago_constants "github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	formatter "github.com/onsi/ginkgo/v2/formatter"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:        "runner",
	Short:      "avalanche-network-runner wrapper",
	SuggestFor: []string{"network-runner"},
	RunE:       runFunc,
}

func init() {
	cobra.EnablePrefixMatching = true
}

var (
	avalancheGoBinPath string
	vmID               string
	vmGenesisPath      string
	outputPath         string
)

func init() {
	rootCmd.PersistentFlags().StringVar(
		&avalancheGoBinPath,
		"avalanchego-path",
		"",
		"avalanchego binary path",
	)
	rootCmd.PersistentFlags().StringVar(
		&vmID,
		"vm-id",
		"",
		"VM ID (must be formatted ids.ID)",
	)
	rootCmd.PersistentFlags().StringVar(
		&vmGenesisPath,
		"vm-genesis-path",
		"",
		"VM genesis file path",
	)
	rootCmd.PersistentFlags().StringVar(
		&outputPath,
		"output-path",
		"",
		"output YAML path to write local cluster information",
	)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "runner failed %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func runFunc(cmd *cobra.Command, args []string) error {
	return run(
		avalancheGoBinPath,
		"subnetevm",
		vmID,
		vmGenesisPath,
		outputPath,
	)
}

func run(
	avalancheGoBinPath string,
	vmName string,
	vmID string,
	vmGenesisPath string,
	outputPath string) (err error) {
	lc := newLocalNetwork(avalancheGoBinPath, vmName, vmID, vmGenesisPath, outputPath)

	go lc.start()
	select {
	case <-lc.readyc:
		outf("{{green}}cluster is ready, waiting for signal/error{{/}}\n")
	case s := <-lc.sigc:
		outf("{{red}}received signal %v before ready, shutting down{{/}}\n", s)
		lc.shutdown()
		return nil
	}
	select {
	case s := <-lc.sigc:
		outf("{{red}}received signal %v, shutting down{{/}}\n", s)
	case err = <-lc.errc:
		outf("{{red}}received error %v, shutting down{{/}}\n", err)
	}

	lc.shutdown()
	return err
}

type localNetwork struct {
	logger  logging.Logger
	logsDir string

	cfg network.Config

	binPath       string
	vmName        string
	vmID          string
	vmGenesisPath string
	outputPath    string

	nw network.Network

	nodes     map[string]node.Node
	nodeNames []string
	nodeIDs   map[string]string
	uris      map[string]string
	apiClis   map[string]api.Client

	pchainFundedAddr string

	subnetTxID   ids.ID // tx ID for "create subnet"
	blkChainTxID ids.ID // tx ID for "create blockchain"

	readyc          chan struct{} // closed when local network is ready/healthy
	readycCloseOnce sync.Once

	sigc  chan os.Signal
	stopc chan struct{}
	donec chan struct{}
	errc  chan error
}

func newLocalNetwork(
	avalancheGoBinPath string,
	vmName string,
	vmID string,
	vmGenesisPath string,
	outputPath string,
) *localNetwork {
	lcfg, err := logging.DefaultConfig()
	if err != nil {
		panic(err)
	}
	logFactory := logging.NewFactory(lcfg)
	logger, err := logFactory.Make("main")
	if err != nil {
		panic(err)
	}

	logsDir, err := ioutil.TempDir(os.TempDir(), "runnerlogs")
	if err != nil {
		panic(err)
	}

	cfg := local.NewDefaultConfig(avalancheGoBinPath)
	nodeNames := make([]string, len(cfg.NodeConfigs))
	for i := range cfg.NodeConfigs {
		nodeName := fmt.Sprintf("node%d", i+1)

		nodeNames[i] = nodeName
		cfg.NodeConfigs[i].Name = nodeName

		// need to whitelist subnet ID to create custom VM chain
		// ref. vms/platformvm/createChain
		cfg.NodeConfigs[i].ConfigFile = []byte(fmt.Sprintf(`{
	"network-peer-list-gossip-frequency":"250ms",
	"network-max-reconnect-delay":"1s",
	"public-ip":"0.0.0.0",
	"http-host":"",
	"health-check-frequency":"2s",
	"api-admin-enabled":true,
	"api-ipcs-enabled":true,
	"index-enabled":true,
	"log-display-level":"INFO",
	"log-level":"INFO",
	"log-dir":"%s",
	"whitelisted-subnets":"%s"
}`,
			filepath.Join(logsDir, nodeName),
			expectedSubnetTxID,
		))
		wr := &writer{
			c:    colors[i%len(cfg.NodeConfigs)],
			name: nodeName,
			w:    os.Stdout,
		}
		cfg.NodeConfigs[i].ImplSpecificConfig = local.NodeConfig{
			BinaryPath: avalancheGoBinPath,
			Stdout:     wr,
			Stderr:     wr,
		}
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	return &localNetwork{
		logger:  logger,
		logsDir: logsDir,

		cfg: cfg,

		binPath:       avalancheGoBinPath,
		vmName:        vmName,
		vmID:          vmID,
		vmGenesisPath: vmGenesisPath,
		outputPath:    outputPath,

		nodeNames: nodeNames,
		nodeIDs:   make(map[string]string),
		uris:      make(map[string]string),
		apiClis:   make(map[string]api.Client),

		readyc: make(chan struct{}),
		sigc:   sigc,
		stopc:  make(chan struct{}),
		donec:  make(chan struct{}),
		errc:   make(chan error, 1),
	}
}

func (lc *localNetwork) start() {
	defer func() {
		close(lc.donec)
	}()

	outf("{{blue}}{{bold}}create and run local network with log-dir %q{{/}}\n", lc.logsDir)
	nw, err := local.NewNetwork(lc.logger, lc.cfg)
	if err != nil {
		lc.errc <- err
		return
	}
	lc.nw = nw

	if err := lc.waitForHealthy(); err != nil {
		lc.errc <- err
		return
	}

	if err := lc.createUser(); err != nil {
		lc.errc <- err
		return
	}
	if err := lc.importKeysAndFunds(); err != nil {
		lc.errc <- err
		return
	}

	if err := lc.createSubnet(); err != nil {
		lc.errc <- err
		return
	}
	for _, name := range lc.nodeNames {
		if err := lc.checkPChainTx(name, lc.subnetTxID); err != nil {
			lc.errc <- err
			return
		}
		if err := lc.checkSubnet(name); err != nil {
			lc.errc <- err
			return
		}
	}
	if err := lc.addSubnetValidators(); err != nil {
		lc.errc <- err
		return
	}
	if err := lc.createBlockchain(); err != nil {
		lc.errc <- err
		return
	}
	for _, name := range lc.nodeNames {
		if err := lc.checkPChainTx(name, lc.blkChainTxID); err != nil {
			lc.errc <- err
			return
		}
		if err := lc.checkBlockchain(name); err != nil {
			lc.errc <- err
			return
		}
	}
	for _, name := range lc.nodeNames {
		if err := lc.checkBootstrapped(name); err != nil {
			lc.errc <- err
			return
		}
	}

	if err := lc.writeOutput(); err != nil {
		lc.errc <- err
		return
	}
}

const (
	genesisPrivKey = "PrivateKey-ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN"

	healthyWait   = 2 * time.Minute
	txConfirmWait = 15 * time.Minute // injecting airdrop accounts can take some time
	checkInterval = time.Second

	validatorWeight    = 50
	validatorStartDiff = 30 * time.Second
	validatorEndDiff   = 30 * 24 * time.Hour // 30 days
)

var errAborted = errors.New("aborted")

func (lc *localNetwork) waitForHealthy() error {
	outf("{{blue}}{{bold}}waiting for all nodes to report healthy...{{/}}\n")

	ctx, cancel := context.WithTimeout(context.Background(), healthyWait)
	defer cancel()
	hc := lc.nw.Healthy(ctx)
	select {
	case <-lc.stopc:
		return errAborted
	case <-ctx.Done():
		return ctx.Err()
	case err := <-hc:
		if err != nil {
			return err
		}
	}

	nodes, err := lc.nw.GetAllNodes()
	if err != nil {
		return err
	}
	lc.nodes = nodes

	for name, node := range nodes {
		nodeID := node.GetNodeID().PrefixedString(avago_constants.NodeIDPrefix)
		lc.nodeIDs[name] = nodeID

		uri := fmt.Sprintf("http://%s:%d", node.GetURL(), node.GetAPIPort())
		lc.uris[name] = uri

		lc.apiClis[name] = node.GetAPIClient()
		outf("{{cyan}}%s: node ID %q, URI %q{{/}}\n", name, nodeID, uri)
	}

	lc.readycCloseOnce.Do(func() {
		close(lc.readyc)
	})
	return nil
}

var (
	// need to hard-code user-pass in order to
	// determine subnet ID for whitelisting
	userPass = avago_api.UserPass{
		Username: "test",
		Password: "vmsrkewl",
	}

	// expected response from "ImportKey"
	// based on hard-coded "userPass" and "genesisPrivKey"
	expectedPchainFundedAddr = "P-custom18jma8ppw3nhx5r4ap8clazz0dps7rv5u9xde7p"

	// expected response from "CreateSubnet"
	// based on hard-coded "userPass" and "pchainFundedAddr"
	expectedSubnetTxID = "24tZhrm8j8GCJRE9PomW8FaeqbgGS4UAQjJnqqn8pq5NwYSYV1"
)

func (lc *localNetwork) createUser() error {
	outf("{{blue}}{{bold}}setting up the same user in all nodes...{{/}}\n")
	for name, cli := range lc.apiClis {
		ok, err := cli.KeystoreAPI().CreateUser(userPass)
		if !ok || err != nil {
			return fmt.Errorf("failedt to create user: %w in %q", err, name)
		}
	}
	return nil
}

func (lc *localNetwork) importKeysAndFunds() error {
	outf("{{blue}}{{bold}}importing genesis key and funds to the user in all nodes...{{/}}\n")
	for _, name := range lc.nodeNames {
		cli := lc.apiClis[name]

		pAddr, err := cli.PChainAPI().ImportKey(userPass, genesisPrivKey)
		if err != nil {
			return fmt.Errorf("failed to import genesis key for P-chain: %w in %q", err, name)
		}
		lc.pchainFundedAddr = pAddr
		if lc.pchainFundedAddr != expectedPchainFundedAddr {
			return fmt.Errorf("unexpected P-chain funded address %q (expected %q)", lc.pchainFundedAddr, expectedPchainFundedAddr)
		}
		pBalance, err := cli.PChainAPI().GetBalance(pAddr)
		if err != nil {
			return fmt.Errorf("failed to get P-chain balance: %w in %q", err, name)
		}
		outf("{{cyan}}funded P-chain: address %q, balance %d $AVAX in %q{{/}}\n", pAddr, pBalance.Balance, name)
	}

	return nil
}

func (lc *localNetwork) createSubnet() error {
	outf("{{blue}}{{bold}}creating subnet...{{/}}\n")
	name := lc.nodeNames[0]
	cli := lc.apiClis[name]
	subnetTxID, err := cli.PChainAPI().CreateSubnet(
		userPass,
		[]string{lc.pchainFundedAddr}, // from
		lc.pchainFundedAddr,           // changeAddr
		[]string{lc.pchainFundedAddr}, // controlKeys
		1,                             // threshold
	)
	if err != nil {
		return fmt.Errorf("failed to create subnet: %w in %q", err, name)
	}
	lc.subnetTxID = subnetTxID
	if lc.subnetTxID.String() != expectedSubnetTxID {
		return fmt.Errorf("unexpected subnet tx ID %q (expected %q)", lc.subnetTxID, expectedSubnetTxID)
	}

	outf("{{blue}}{{bold}}created subnet %q in %q{{/}}\n", subnetTxID, name)
	return nil
}

func (lc *localNetwork) checkPChainTx(name string, txID ids.ID) error {
	outf("{{blue}}{{bold}}checking tx %q in %q{{/}}\n", txID, name)
	cli, ok := lc.apiClis[name]
	if !ok {
		return fmt.Errorf("%q API client not found", name)
	}
	pcli := cli.PChainAPI()

	ctx, cancel := context.WithTimeout(context.Background(), txConfirmWait)
	defer cancel()
	for ctx.Err() == nil {
		select {
		case <-lc.stopc:
			return errAborted
		case <-time.After(checkInterval):
		}

		status, err := pcli.GetTxStatus(txID, true)
		if err != nil {
			outf("{{yellow}}failed to get tx status %v in %q{{/}}\n", err, name)
			continue
		}
		if status.Status != platformvm.Committed {
			outf("{{yellow}}subnet tx %s status %q in %q{{/}}\n", txID, status.Status, name)
			continue
		}

		outf("{{cyan}}confirmed tx %q %q in %q{{/}}\n", txID, status.Status, name)
		return nil
	}
	return ctx.Err()
}

func (lc *localNetwork) checkSubnet(name string) error {
	outf("{{blue}}{{bold}}checking subnet exists %q in %q{{/}}\n", lc.subnetTxID, name)
	cli, ok := lc.apiClis[name]
	if !ok {
		return fmt.Errorf("%q API client not found", name)
	}
	pcli := cli.PChainAPI()

	ctx, cancel := context.WithTimeout(context.Background(), txConfirmWait)
	defer cancel()
	for ctx.Err() == nil {
		select {
		case <-lc.stopc:
			return errAborted
		case <-time.After(checkInterval):
		}

		subnets, err := pcli.GetSubnets([]ids.ID{})
		if err != nil {
			outf("{{yellow}}failed to get subnets %v in %q{{/}}\n", err, name)
			continue
		}

		found := false
		for _, sub := range subnets {
			if sub.ID == lc.subnetTxID {
				found = true
				outf("{{cyan}}%q returned expected subnet ID %q{{/}}\n", name, sub.ID)
				break
			}
			outf("{{yellow}}%q returned unexpected subnet ID %q{{/}}\n", name, sub.ID)
		}
		if !found {
			outf("{{yellow}}%q does not have subnet %q{{/}}\n", name, lc.subnetTxID)
			continue
		}

		outf("{{cyan}}confirmed subnet exists %q in %q{{/}}\n", lc.subnetTxID, name)
		return nil
	}
	return ctx.Err()
}

func (lc *localNetwork) addSubnetValidators() error {
	outf("{{blue}}{{bold}}adding subnet validator...{{/}}\n")
	for name, cli := range lc.apiClis {
		valTxID, err := cli.PChainAPI().AddSubnetValidator(
			userPass,
			[]string{lc.pchainFundedAddr}, // from
			lc.pchainFundedAddr,           // changeAddr
			lc.subnetTxID.String(),        // subnetID
			lc.nodeIDs[name],              // nodeID
			validatorWeight,               // stakeAmount
			uint64(time.Now().Add(validatorStartDiff).Unix()), // startTime
			uint64(time.Now().Add(validatorEndDiff).Unix()),   // endTime
		)
		if err != nil {
			return fmt.Errorf("failed to add subnet validator: %w in %q", err, name)
		}
		if err := lc.checkPChainTx(name, valTxID); err != nil {
			return err
		}
		outf("{{cyan}}added subnet validator %q in %q{{/}}\n", valTxID, name)
	}
	return nil
}

func (lc *localNetwork) createBlockchain() error {
	vmGenesis, err := ioutil.ReadFile(lc.vmGenesisPath)
	if err != nil {
		return fmt.Errorf("failed to read genesis file (%s): %w", lc.vmGenesisPath, err)
	}

	outf("{{blue}}{{bold}}creating blockchain with vm name %q and ID %q...{{/}}\n", lc.vmName, lc.vmID)
	for name, cli := range lc.apiClis {
		blkChainTxID, err := cli.PChainAPI().CreateBlockchain(
			userPass,
			[]string{lc.pchainFundedAddr}, // from
			lc.pchainFundedAddr,           // changeAddr
			lc.subnetTxID,                 // subnetID
			lc.vmID,                       // vmID
			[]string{},                    // fxIDs
			lc.vmName,                     // name
			vmGenesis,                     // genesisData
		)
		if err != nil {
			return fmt.Errorf("failed to create blockchain: %w in %q", err, name)
		}
		lc.blkChainTxID = blkChainTxID
		outf("{{blue}}{{bold}}created blockchain %q in %q{{/}}\n", blkChainTxID, name)
		break
	}
	return nil
}

func (lc *localNetwork) checkBlockchain(name string) error {
	outf("{{blue}}{{bold}}checking blockchain exists %q in %q{{/}}\n", lc.blkChainTxID, name)
	cli, ok := lc.apiClis[name]
	if !ok {
		return fmt.Errorf("%q API client not found", name)
	}
	pcli := cli.PChainAPI()

	ctx, cancel := context.WithTimeout(context.Background(), txConfirmWait)
	defer cancel()
	for ctx.Err() == nil {
		select {
		case <-lc.stopc:
			return errAborted
		case <-time.After(checkInterval):
		}

		blockchains, err := pcli.GetBlockchains()
		if err != nil {
			outf("{{yellow}}failed to get blockchains %v in %q{{/}}\n", err, name)
			continue
		}
		blockchainID := ids.Empty
		for _, blockchain := range blockchains {
			if blockchain.SubnetID == lc.subnetTxID {
				blockchainID = blockchain.ID
				break
			}
		}
		if blockchainID == ids.Empty {
			outf("{{yellow}}failed to get blockchain ID in %q{{/}}\n", name)
			continue
		}
		if lc.blkChainTxID != blockchainID {
			outf("{{yellow}}unexpected blockchain ID %q in %q{{/}} (expected %q)\n", name, lc.blkChainTxID)
			continue
		}

		status, err := pcli.GetBlockchainStatus(blockchainID.String())
		if err != nil {
			outf("{{yellow}}failed to get blockchain status %v in %q{{/}}\n", err, name)
			continue
		}
		if status != platformvm.Validating {
			outf("{{yellow}}blockchain status %q in %q, retrying{{/}}\n", status, name)
			continue
		}

		outf("{{cyan}}confirmed blockchain exists and status %q in %q{{/}}\n", status, name)
		return nil
	}
	return ctx.Err()
}

func (lc *localNetwork) checkBootstrapped(name string) error {
	outf("{{blue}}{{bold}}checking blockchain bootstrapped %q in %q{{/}}\n", lc.blkChainTxID, name)
	cli, ok := lc.apiClis[name]
	if !ok {
		return fmt.Errorf("%q API client not found", name)
	}
	icli := cli.InfoAPI()

	ctx, cancel := context.WithTimeout(context.Background(), txConfirmWait)
	defer cancel()
	for ctx.Err() == nil {
		select {
		case <-lc.stopc:
			return errAborted
		case <-time.After(checkInterval):
		}

		bootstrapped, err := icli.IsBootstrapped(lc.blkChainTxID.String())
		if err != nil {
			outf("{{yellow}}failed to check blockchain bootstrapped %v in %q{{/}}\n", err, name)
			continue
		}
		if !bootstrapped {
			outf("{{yellow}}blockchain %q in %q not bootstrapped yet{{/}}\n", lc.blkChainTxID, name)
			continue
		}

		outf("{{cyan}}confirmed blockchain bootstrapped %q in %q{{/}}\n", lc.blkChainTxID, name)
		return nil
	}
	return ctx.Err()
}

func (lc *localNetwork) getURIs() []string {
	uris := make([]string, 0, len(lc.uris))
	for _, u := range lc.uris {
		uris = append(uris, u)
	}
	sort.Strings(uris)
	return uris
}

func (lc *localNetwork) writeOutput() error {
	pid := os.Getpid()
	outf("{{blue}}{{bold}}writing output %q with PID %d{{/}}\n", lc.outputPath, pid)
	ci := ClusterInfo{
		URIs:     lc.getURIs(),
		Endpoint: fmt.Sprintf("/ext/bc/%s", lc.blkChainTxID),
		PID:      pid,
		LogsDir:  lc.logsDir,
	}
	err := ci.Save(lc.outputPath)
	if err != nil {
		return err
	}

	b, err := ioutil.ReadFile(lc.outputPath)
	if err != nil {
		return err
	}
	outf("\n{{blue}}$ cat %s:{{/}}\n%s\n", lc.outputPath, string(b))
	return nil
}

func (lc *localNetwork) shutdown() {
	close(lc.stopc)
	serr := lc.nw.Stop(context.Background())
	<-lc.donec
	outf("{{red}}{{bold}}terminated network{{/}} (error %v)\n", serr)
}

// https://github.com/onsi/ginkgo/blob/v2.0.0/formatter/formatter.go#L52-L73
func outf(format string, args ...interface{}) {
	s := formatter.F(format, args...)
	fmt.Fprint(formatter.ColorableStdOut, s)
}

type writer struct {
	c    string
	name string
	w    io.Writer
}

var colors = []string{
	"{{green}}",
	"{{yellow}}",
	"{{blue}}",
	"{{magenta}}",
	"{{cyan}}",
}

func (wr *writer) Write(p []byte) (n int, err error) {
	s := formatter.F(wr.c+"[%s]{{/}}	", wr.name)
	fmt.Fprint(formatter.ColorableStdOut, s)
	return wr.w.Write(p)
}
