// Command erc20bench is a one-shot, self-contained throughput comparison of
// three ways to run a USDC-shaped token on an Avalanche L1:
//
//	level 1: the token in Solidity, one transaction per transfer
//	level 2: the same token as a stateful precompile, one transaction per transfer
//	level 3: the precompile's batchTransfer: one transaction carries many
//	         EIP-712-signed transfer authorizations (the gasless MetaMask flow)
//
// It boots a local devnet with tmpnet (5 nodes by default), creates one L1
// whose genesis has gas configured to never be the bottleneck (200M gas
// blocks, 25ms min block delay, flat base fee), deploys the Solidity token,
// activates the precompile from genesis, runs the selected levels back to
// back against the same chain, and prints transfers per second for each.
//
// Run it via run.sh, which builds avalanchego and the subnet-evm plugin first.
package main

import (
	"cmp"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/rpc"

	_ "embed"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/bencherc20"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
)

//go:embed BenchToken.bin
var benchTokenBinHex string

var chainID = big.NewInt(bencherc20.EVMChainID)

const (
	gasPrice        = 1_000_000_000 // 1 gwei against a flat 1 wei base fee
	transferGas     = 90_000
	setupGas        = 3_000_000
	maxInflight     = 64 // per sender: issued nonces ahead of last mined. The block builder waits for the txpool to finish reorging to each new head, and that reorg is O(pending), so the pending pool must stay shallow: 64 senders x 64 = ~4k pending, a few blocks of backlog
	sendBatchSize   = 128  // eth_sendRawTransaction calls per JSON-RPC batch
	warmupSeconds   = 5
	oneToken        = 1 // amount moved per transfer
	receiptSamples  = 10
	sampleEveryNSent = 1000
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		levelsFlag  = flag.String("levels", "1,2,3", "Comma-separated benchmark levels to run in order")
		duration    = flag.Duration("duration", 30*time.Second, "Measured load duration per level")
		nodeCount   = flag.Int("nodes", 5, "Devnet node count; all validate the L1")
		senderCount = flag.Int("senders", 64, "EOA accounts issuing transactions (relayers at level 3)")
		userCount   = flag.Int("users", 512, "Accounts that sign EIP-712 transfer authorizations at level 3")
		batchSize   = flag.Int("batch", 50, "Signed transfers per batchTransfer transaction at level 3")
		avagoPath   = flag.String("avalanchego", "build/avalanchego", "avalanchego binary (built by run.sh)")
		pluginDir   = flag.String("plugin-dir", "build/plugins", "Plugin dir holding the subnet-evm binary (built by run.sh)")
		keep        = flag.Bool("keep", false, "Leave the devnet running after the benchmark")
		profileDir  = flag.String("profile-dir", "", "If set, nodes run subnet-evm's continuous CPU profiler (45s windows) writing here; use with -nodes 1")
		stateScheme = flag.String("state-scheme", "hashdb", "State database scheme: hashdb or firewood")
	)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()

	absAvago, err := filepath.Abs(*avagoPath)
	if err != nil {
		return err
	}
	absPlugins, err := filepath.Abs(*pluginDir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(absAvago); err != nil {
		return fmt.Errorf("avalanchego binary missing (run via run.sh): %w", err)
	}

	// Deterministic actors. Only treasury and senders hold native coin; level 3
	// users are gasless and never send a transaction themselves.
	treasury := benchKey("treasury", 0)
	senders := benchKeys("sender", *senderCount)
	users := benchKeys("user", *userCount)
	recipients := make([]common.Address, *userCount)
	for i := range recipients {
		recipients[i] = crypto.PubkeyToAddress(benchKey("recipient", i).PublicKey)
	}

	genesisBytes, err := chainGenesis(treasury, senders)
	if err != nil {
		return err
	}

	log := logging.NewLogger("erc20bench", logging.NewWrappedCore(logging.Info, os.Stdout, logging.Plain.ConsoleEncoder()))

	nodes := tmpnet.NewNodesOrPanic(*nodeCount)
	network := &tmpnet.Network{
		Owner: "erc20bench",
		Nodes: nodes,
		Subnets: []*tmpnet.Subnet{{
			Name: "erc20bench",
			Config: tmpnet.ConfigMap{
				"snowParameters": map[string]any{
					"k":               *nodeCount,
					"alphaPreference": *nodeCount/2 + 1,
					"alphaConfidence": *nodeCount/2 + 1,
					"beta":            4,
				},
				// Millisecond proposer timestamps; a 2s window budgets for the
				// worst-case block build so validators do not propose
				// competing sibling blocks.
				"proposerWindowMilliseconds":    100,
				"proposerMillisecondTimestamps": true,
			},
			Chains: []*tmpnet.Chain{{
				VMID:    constants.SubnetEVMID,
				Genesis: genesisBytes,
				Config:  chainConfig(*profileDir, *stateScheme),
			}},
			ValidatorIDs: tmpnet.NodesToIDs(nodes...),
		}},
		// Multi-MB blocks queue behind the default bandwidth throttler; give
		// the local devnet enough headroom that gossip is never the limiter.
		DefaultFlags: tmpnet.FlagsMap{
			"log-level":                                  cmp.Or(os.Getenv("ERC20BENCH_LOG_LEVEL"), "info"),
			"consensus-frontier-poll-frequency":          "10ms",
			"throttler-inbound-bandwidth-refill-rate":    "67108864",
			"throttler-inbound-bandwidth-max-burst-size": "134217728",
			"throttler-outbound-at-large-alloc-size":     "134217728",
			"throttler-outbound-node-max-at-large-bytes": "67108864",
		},
		DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{
				AvalancheGoPath: absAvago,
				PluginDir:       absPlugins,
			},
		},
	}

	fmt.Printf("booting %d-node devnet...\n", *nodeCount)
	if err := tmpnet.BootstrapNewNetwork(ctx, log, network, ""); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	if !*keep {
		defer func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Minute)
			defer stopCancel()
			if err := network.Stop(stopCtx); err != nil {
				fmt.Fprintln(os.Stderr, "network stop:", err)
			}
		}()
	}

	blockchainID := network.Subnets[0].Chains[0].ChainID
	rpcURLs := make([]string, len(network.Nodes))
	for i, node := range network.Nodes {
		rpcURLs[i] = fmt.Sprintf("%s/ext/bc/%s/rpc", node.GetAccessibleURI(), blockchainID)
	}
	fmt.Printf("devnet up, network dir %s\nchain %s\nrpc %s\n", network.Dir, blockchainID, rpcURLs[0])

	clients := make([]*rpc.Client, len(rpcURLs))
	for i, url := range rpcURLs {
		if clients[i], err = waitForRPC(ctx, url); err != nil {
			return err
		}
	}
	eth := ethclient.NewClient(clients[0])

	// Setup: deploy the Solidity token, then mint on both tokens.
	setup := &account{key: treasury, client: clients[0], eth: eth}
	if err := setup.init(ctx); err != nil {
		return err
	}
	fmt.Println("deploying Solidity BenchToken...")
	deployHash, err := setup.send(ctx, nil, common.FromHex(strings.TrimSpace(benchTokenBinHex)), setupGas)
	if err != nil {
		return err
	}
	deployReceipt, err := waitMined(ctx, eth, deployHash)
	if err != nil {
		return err
	}
	solidityToken := deployReceipt.ContractAddress
	fmt.Printf("BenchToken at %s\n", solidityToken)

	fmt.Println("minting balances on both tokens...")
	mintAmount := new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)
	var lastMint common.Hash
	for _, s := range senders {
		addr := crypto.PubkeyToAddress(s.PublicKey)
		for _, token := range []common.Address{solidityToken, bencherc20.ContractAddress} {
			if lastMint, err = setup.send(ctx, &token, mustPack("mint", addr, mintAmount), transferGas); err != nil {
				return err
			}
		}
	}
	for _, u := range users {
		addr := crypto.PubkeyToAddress(u.PublicKey)
		token := bencherc20.ContractAddress
		if lastMint, err = setup.send(ctx, &token, mustPack("mint", addr, mintAmount), transferGas); err != nil {
			return err
		}
	}
	if _, err := waitMined(ctx, eth, lastMint); err != nil {
		return err
	}
	if err := checkBalance(ctx, clients[0], solidityToken, crypto.PubkeyToAddress(senders[0].PublicKey), mintAmount); err != nil {
		return err
	}
	if err := checkBalance(ctx, clients[0], bencherc20.ContractAddress, crypto.PubkeyToAddress(users[0].PublicKey), mintAmount); err != nil {
		return err
	}
	fmt.Println("setup complete")

	var results []levelResult
	for _, levelStr := range strings.Split(*levelsFlag, ",") {
		level, err := strconv.Atoi(strings.TrimSpace(levelStr))
		if err != nil || level < 1 || level > 3 {
			return fmt.Errorf("bad level %q", levelStr)
		}
		result, err := runLevel(ctx, level, levelConfig{
			duration:      *duration,
			batchSize:     *batchSize,
			senders:       senders,
			users:         users,
			recipients:    recipients,
			solidityToken: solidityToken,
			clients:       clients,
			eth:           eth,
		})
		if err != nil {
			return fmt.Errorf("level %d: %w", level, err)
		}
		results = append(results, result)
	}

	fmt.Println("\n=== RESULTS ===")
	for _, r := range results {
		fmt.Printf("level %d %-22s %9.0f transfers/s  (%6.0f txs/s, %5.1f blocks/s, %6.0f txs/block, %4.1fM gas/block, %d blocks measured)\n",
			r.level, levelName(r.level), r.transfersPerSec, r.txsPerSec, r.blocksPerSec, r.txsPerBlock, r.gasPerBlock/1e6, r.blocks)
	}
	if *keep {
		fmt.Printf("\ndevnet left running in %s (stop with: go run ./tests/fixture/tmpnet/cmd -- stop or kill the processes)\n", network.Dir)
	}
	return nil
}

func levelName(level int) string {
	switch level {
	case 1:
		return "solidity ERC-20:"
	case 2:
		return "precompile ERC-20:"
	default:
		return "precompile batch:"
	}
}

// --- genesis and chain config ---

// chainConfigJSON mirrors the delivery fleet's validator chain config: keep
// the 25ms delay target and give the mempool room for a single-machine flood.
// Caches are modest because several nodes share one box.
const chainConfigJSON = `{
	"min-delay-target": 25,
	"push-gossip-frequency": "20ms",
	"max-outbound-active-requests": 64,
	"pruning-enabled": true,
	"tx-pool-account-slots": 131072,
	"tx-pool-global-slots": 262144,
	"tx-pool-account-queue": 131072,
	"tx-pool-global-queue": 512000,
	"tx-pool-lifetime": "10m",
	"trie-clean-cache": 512,
	"trie-dirty-cache": 512,
	"snapshot-cache": 512
}`

// chainConfig returns chainConfigJSON, optionally with subnet-evm's
// continuous CPU profiler enabled (it runs in the plugin process, which the
// avalanchego admin profiler cannot see).
func chainConfig(profileDir, stateScheme string) string {
	cfg := map[string]any{}
	if err := json.Unmarshal([]byte(chainConfigJSON), &cfg); err != nil {
		panic(err)
	}
	if profileDir != "" {
		cfg["continuous-profiler-dir"] = profileDir
		cfg["continuous-profiler-frequency"] = "45s"
		cfg["continuous-profiler-max-files"] = 20
	}
	if stateScheme != "hashdb" {
		cfg["state-scheme"] = stateScheme
		// Firewood has no iterator support, so the snapshot layer must be off.
		cfg["snapshot-cache"] = 0
	}
	out, err := json.Marshal(cfg)
	if err != nil {
		panic(err)
	}
	return string(out)
}

// chainGenesis produces the L1 genesis: gas can never be the bottleneck
// (200M gas blocks, flat 1 wei base fee, 25ms ACP-226 seed), the bench
// precompile is active from genesis with the treasury as owner, and native
// coin is allocated to every account that must pay for gas.
func chainGenesis(treasury *ecdsa.PrivateKey, senders []*ecdsa.PrivateKey) ([]byte, error) {
	nativeBalance := "0xd3c21bcecceda1000000" // 10^24 wei
	alloc := map[string]any{
		crypto.PubkeyToAddress(treasury.PublicKey).Hex(): map[string]any{"balance": nativeBalance},
	}
	for _, s := range senders {
		alloc[crypto.PubkeyToAddress(s.PublicKey).Hex()] = map[string]any{"balance": nativeBalance}
	}
	genesis := map[string]any{
		"config": map[string]any{
			"chainId":           bencherc20.EVMChainID,
			"graniteTimestamp":  0,
			"initialMinDelayMS": 25,
			"feeConfig": map[string]any{
				"gasLimit":                 50_000_000,
				"targetBlockRate":          1,
				"minBaseFee":               1,
				"targetGas":                uint64(1<<64 - 1),
				"baseFeeChangeDenominator": uint64(1<<63 - 1),
				"minBlockGasCost":          0,
				"maxBlockGasCost":          0,
				"blockGasCostStep":         0,
			},
			bencherc20.ConfigKey: map[string]any{
				"blockTimestamp": 0,
				"owner":          crypto.PubkeyToAddress(treasury.PublicKey).Hex(),
			},
		},
		"alloc": alloc,
		"nonce": "0x0",
		// Granite must be active AT the genesis timestamp or the
		// initialMinDelayMS seed is skipped (IsGranite(genesisTime) is false
		// for a 1970 genesis even on local networks) and the chain starts at
		// the ~2000ms ACP-226 default, converging to 25ms only after ~23k
		// blocks at 200 excess units per block.
		"timestamp": fmt.Sprintf("0x%x", time.Now().Add(-time.Minute).Unix()),
		"extraData":  "0x00",
		"gasLimit":   "0x2faf080",
		"difficulty": "0x0",
		"mixHash":    "0x0000000000000000000000000000000000000000000000000000000000000000",
		"coinbase":   "0x0000000000000000000000000000000000000000",
		"number":     "0x0",
		"gasUsed":    "0x0",
		"parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
	}
	return json.Marshal(genesis)
}

// --- keys ---

func benchKey(role string, i int) *ecdsa.PrivateKey {
	for salt := 0; ; salt++ {
		seed := crypto.Keccak256([]byte(fmt.Sprintf("erc20bench/%s/%d/%d", role, i, salt)))
		if key, err := crypto.ToECDSA(seed); err == nil {
			return key
		}
	}
}

func benchKeys(role string, count int) []*ecdsa.PrivateKey {
	keys := make([]*ecdsa.PrivateKey, count)
	for i := range keys {
		keys[i] = benchKey(role, i)
	}
	return keys
}

// --- setup helpers ---

func mustPack(method string, args ...any) []byte {
	data, err := bencherc20.ABI.Pack(method, args...)
	if err != nil {
		panic(err)
	}
	return data
}

type account struct {
	key    *ecdsa.PrivateKey
	client *rpc.Client
	eth    *ethclient.Client
	nonce  uint64
}

func (a *account) init(ctx context.Context) error {
	nonce, err := a.eth.NonceAt(ctx, crypto.PubkeyToAddress(a.key.PublicKey), nil)
	a.nonce = nonce
	return err
}

func (a *account) send(ctx context.Context, to *common.Address, data []byte, gas uint64) (common.Hash, error) {
	tx, err := types.SignNewTx(a.key, types.LatestSignerForChainID(chainID), &types.LegacyTx{
		Nonce:    a.nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      gas,
		To:       to,
		Data:     data,
	})
	if err != nil {
		return common.Hash{}, err
	}
	if err := a.eth.SendTransaction(ctx, tx); err != nil {
		return common.Hash{}, fmt.Errorf("send nonce %d: %w", a.nonce, err)
	}
	a.nonce++
	return tx.Hash(), nil
}

func waitMined(ctx context.Context, eth *ethclient.Client, hash common.Hash) (*types.Receipt, error) {
	for start := time.Now(); ; {
		receipt, err := eth.TransactionReceipt(ctx, hash)
		if err == nil {
			if receipt.Status != types.ReceiptStatusSuccessful {
				return receipt, fmt.Errorf("tx %s reverted", hash)
			}
			return receipt, nil
		}
		if time.Since(start) > 2*time.Minute {
			return nil, fmt.Errorf("tx %s not mined after 2m: %w", hash, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func waitForRPC(ctx context.Context, url string) (*rpc.Client, error) {
	deadline := time.Now().Add(3 * time.Minute)
	for {
		client, err := rpc.DialContext(ctx, url)
		if err == nil {
			var id hexutil.Big
			if err = client.CallContext(ctx, &id, "eth_chainId"); err == nil {
				return client, nil
			}
			client.Close()
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("chain rpc %s not ready: %w", url, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func checkBalance(ctx context.Context, client *rpc.Client, token, account common.Address, want *big.Int) error {
	var result hexutil.Bytes
	err := client.CallContext(ctx, &result, "eth_call", map[string]any{
		"to":   token.Hex(),
		"data": hexutil.Encode(mustPack("balanceOf", account)),
	}, "latest")
	if err != nil {
		return err
	}
	got := new(big.Int).SetBytes(result)
	if got.Cmp(want) < 0 {
		return fmt.Errorf("balance of %s on %s is %s, want at least %s", account, token, got, want)
	}
	return nil
}

// --- benchmark ---

type levelConfig struct {
	duration      time.Duration
	batchSize     int
	senders       []*ecdsa.PrivateKey
	users         []*ecdsa.PrivateKey
	recipients    []common.Address
	solidityToken common.Address
	clients       []*rpc.Client
	eth           *ethclient.Client
}

type levelResult struct {
	level           int
	blocks          int
	transfersPerSec float64
	txsPerSec       float64
	blocksPerSec    float64
	txsPerBlock     float64
	gasPerBlock     float64
}

type blockStat struct {
	txs     int
	gasUsed uint64
	tsMS    uint64
}

func runLevel(ctx context.Context, level int, cfg levelConfig) (levelResult, error) {
	fmt.Printf("\n--- level %d %s %s of load ---\n", level, levelName(level), cfg.duration)

	perTx := 1
	if level == 3 {
		perTx = cfg.batchSize
	}

	startBlock, err := cfg.eth.BlockNumber(ctx)
	if err != nil {
		return levelResult{}, err
	}
	deadline := time.Now().Add(cfg.duration)

	var (
		sent         atomic.Uint64
		sendErrs     atomic.Uint64
		firstErr     firstError
		sampleMu     sync.Mutex
		sampleHashes []common.Hash
	)
	recordSample := func(hash common.Hash) {
		sampleMu.Lock()
		if len(sampleHashes) < receiptSamples*10 {
			sampleHashes = append(sampleHashes, hash)
		}
		sampleMu.Unlock()
	}

	var wg sync.WaitGroup
	for i := range cfg.senders {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := senderLoop(ctx, level, i, cfg, deadline, &sent, &sendErrs, &firstErr, recordSample); err != nil {
				firstErr.set(err)
			}
		}(i)
	}

	stats, err := watchBlocks(ctx, cfg.clients[0], startBlock+1, deadline)
	wg.Wait()
	if err != nil {
		return levelResult{}, err
	}
	if len(stats) > 0 {
		first, last := stats[0], stats[len(stats)-1]
		fmt.Printf("observed %d blocks, ts %d..%d (%.1fs), txs total %d\n",
			len(stats), first.tsMS, last.tsMS, float64(last.tsMS-first.tsMS)/1000, totalTxs(stats))
	}
	if err := firstErr.get(); err != nil {
		return levelResult{}, err
	}
	fmt.Printf("issued %d txs (%d send errors)\n", sent.Load(), sendErrs.Load())

	// Verify a sample of receipts: the numbers are meaningless if txs revert.
	sampleMu.Lock()
	samples := sampleHashes
	sampleMu.Unlock()
	if len(samples) == 0 {
		return levelResult{}, fmt.Errorf("no transactions issued")
	}
	step := max(1, len(samples)/receiptSamples)
	for i := 0; i < len(samples); i += step {
		receipt, err := waitMined(ctx, cfg.eth, samples[i])
		if err != nil {
			return levelResult{}, fmt.Errorf("sampled receipt: %w", err)
		}
		if level == 3 && len(receipt.Logs) != cfg.batchSize {
			return levelResult{}, fmt.Errorf("batch tx %s has %d transfer logs, want %d", samples[i], len(receipt.Logs), cfg.batchSize)
		}
	}

	// Steady-state window: drop the warmup, then count what the chain mined.
	if len(stats) < 3 {
		return levelResult{}, fmt.Errorf("only %d blocks observed", len(stats))
	}
	windowStart := stats[0].tsMS + warmupSeconds*1000
	deadlineMS := uint64(deadline.UnixMilli())
	var (
		transfers, txs, gasUsed uint64
		firstTS, lastTS         uint64
		blocks                  int
	)
	for _, stat := range stats {
		if stat.tsMS < windowStart || stat.tsMS > deadlineMS {
			continue
		}
		if firstTS == 0 {
			firstTS = stat.tsMS
		}
		lastTS = stat.tsMS
		blocks++
		txs += uint64(stat.txs)
		transfers += uint64(stat.txs * perTx)
		gasUsed += stat.gasUsed
	}
	if blocks < 2 || lastTS <= firstTS {
		return levelResult{}, fmt.Errorf("measurement window too small: %d blocks", blocks)
	}
	spanSec := float64(lastTS-firstTS) / 1000
	result := levelResult{
		level:           level,
		blocks:          blocks,
		transfersPerSec: float64(transfers) / spanSec,
		txsPerSec:       float64(txs) / spanSec,
		blocksPerSec:    float64(blocks-1) / spanSec,
		txsPerBlock:     float64(txs) / float64(blocks),
		gasPerBlock:     float64(gasUsed) / float64(blocks),
	}
	fmt.Printf("level %d: %.0f transfers/s over %d blocks (%.1fs window)\n", level, result.transfersPerSec, blocks, spanSec)
	return result, nil
}

// senderLoop issues signed transactions from one sender account until the
// deadline, in JSON-RPC batches, throttled by how far its nonce is ahead of
// the chain.
func senderLoop(
	ctx context.Context,
	level, senderIndex int,
	cfg levelConfig,
	deadline time.Time,
	sent, sendErrs *atomic.Uint64,
	firstErr *firstError,
	recordSample func(common.Hash),
) error {
	key := cfg.senders[senderIndex]
	address := crypto.PubkeyToAddress(key.PublicKey)
	client := cfg.clients[senderIndex%len(cfg.clients)]
	eth := ethclient.NewClient(client)
	signer := types.LatestSignerForChainID(chainID)

	// A level-3 transaction carries batchSize transfers, so shrink the nonce
	// window and the RPC batch accordingly: target a total backlog of ~2048
	// batch transactions (about five 200M-gas blocks) across all relayers,
	// enough to keep blocks full without an unflushable mempool tail.
	inflight := uint64(maxInflight)
	rpcBatch := sendBatchSize
	if level == 3 {
		inflight = max(1024/uint64(len(cfg.senders)), 4)
		rpcBatch = max(sendBatchSize/8, 8)
	}

	nonce, err := eth.NonceAt(ctx, address, nil)
	if err != nil {
		return err
	}
	minedNonce := nonce

	// Level 3: this relayer signs for its own slice of users so no two
	// relayers race on one user's authorization nonces.
	var relayUsers []*ecdsa.PrivateKey
	var authNonce uint64
	if level == 3 {
		per := len(cfg.users) / len(cfg.senders)
		if per == 0 {
			return fmt.Errorf("need at least one user per sender")
		}
		relayUsers = cfg.users[senderIndex*per : (senderIndex+1)*per]
	}

	counter := 0
	buildPayload := func() (to common.Address, data []byte, gas uint64, err error) {
		switch level {
		case 1, 2:
			token := cfg.solidityToken
			if level == 2 {
				token = bencherc20.ContractAddress
			}
			recipient := cfg.recipients[(senderIndex+counter)%len(cfg.recipients)]
			return token, mustPack("transfer", recipient, big.NewInt(oneToken)), transferGas, nil
		default:
			records := make([]byte, 0, cfg.batchSize*bencherc20.RecordLen)
			for range cfg.batchSize {
				user := relayUsers[int(authNonce)%len(relayUsers)]
				from := crypto.PubkeyToAddress(user.PublicKey)
				to := cfg.recipients[(senderIndex+int(authNonce))%len(cfg.recipients)]
				value := big.NewInt(oneToken)
				nonceHash := common.BigToHash(new(big.Int).SetUint64(uint64(senderIndex)<<40 | authNonce))
				authNonce++
				sig, err := crypto.Sign(bencherc20.AuthDigest(from, to, value, nonceHash).Bytes(), user)
				if err != nil {
					return common.Address{}, nil, 0, err
				}
				records = bencherc20.AppendRecord(records, from, to, value, nonceHash, sig)
			}
			gas := uint64(21_000 + bencherc20.BatchBaseGasCost + cfg.batchSize*(bencherc20.BatchPerTransferGasCost+16*bencherc20.RecordLen) + 100_000)
			return bencherc20.ContractAddress, mustPack("batchTransfer", records), gas, nil
		}
	}

	// Signed-but-unmined raw txs by nonce. Under saturation the preference can
	// flap between duplicate block variants and the txpool transiently rejects
	// or drops transactions; a sender must tolerate send errors and resubmit,
	// or one lost tx wedges its whole nonce chain.
	pending := map[uint64][]byte{}
	sendRaws := func(raws [][]byte) {
		if len(raws) == 0 {
			return
		}
		elems := make([]rpc.BatchElem, len(raws))
		for i, raw := range raws {
			elems[i] = rpc.BatchElem{
				Method: "eth_sendRawTransaction",
				Args:   []any{hexutil.Encode(raw)},
				Result: new(common.Hash),
			}
		}
		if err := client.BatchCallContext(ctx, elems); err != nil {
			sendErrs.Add(1)
			return
		}
		for _, elem := range elems {
			// Resubmission races produce "already known" and "nonce too low";
			// neither is a lost transaction.
			if elem.Error != nil &&
				!strings.Contains(elem.Error.Error(), "already known") &&
				!strings.Contains(elem.Error.Error(), "nonce too low") {
				sendErrs.Add(1)
			}
		}
	}
	trimMined := func() {
		mined, err := eth.NonceAt(ctx, address, nil)
		if err != nil {
			return
		}
		if mined > minedNonce {
			minedNonce = mined
			for n := range pending {
				if n < minedNonce {
					delete(pending, n)
				}
			}
		}
	}
	resubmitHead := func() {
		raws := make([][]byte, 0, rpcBatch)
		for n := minedNonce; n < nonce && len(raws) < rpcBatch; n++ {
			if raw, ok := pending[n]; ok {
				raws = append(raws, raw)
			}
		}
		sendRaws(raws)
	}

	lastProgress := time.Now()
	for time.Now().Before(deadline) {
		if nonce-minedNonce > inflight {
			before := minedNonce
			trimMined()
			if minedNonce > before {
				lastProgress = time.Now()
			}
			if nonce-minedNonce > inflight {
				if time.Since(lastProgress) > 3*time.Second {
					resubmitHead()
					lastProgress = time.Now()
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(20 * time.Millisecond):
				}
				continue
			}
		}

		raws := make([][]byte, 0, rpcBatch)
		for range rpcBatch {
			to, data, gas, err := buildPayload()
			if err != nil {
				return err
			}
			counter++
			tx, err := types.SignNewTx(key, signer, &types.LegacyTx{
				Nonce:    nonce,
				GasPrice: big.NewInt(gasPrice),
				Gas:      gas,
				To:       &to,
				Data:     data,
			})
			if err != nil {
				return err
			}
			raw, err := tx.MarshalBinary()
			if err != nil {
				return err
			}
			pending[nonce] = raw
			nonce++
			if sent.Add(1)%sampleEveryNSent == 1 {
				recordSample(tx.Hash())
			}
			raws = append(raws, raw)
		}
		sendRaws(raws)
	}

	// Flush: make sure everything issued actually mines, resubmitting stalled
	// nonces, so the drain and the receipt sampling see a settled chain.
	flushDeadline := deadline.Add(90 * time.Second)
	for minedNonce < nonce && time.Now().Before(flushDeadline) {
		before := minedNonce
		trimMined()
		if minedNonce > before {
			lastProgress = time.Now()
		} else if time.Since(lastProgress) > 3*time.Second {
			resubmitHead()
			lastProgress = time.Now()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	if minedNonce < nonce {
		return fmt.Errorf("sender %d: %d txs still unmined 90s after deadline", senderIndex, nonce-minedNonce)
	}
	return nil
}

func totalTxs(stats []blockStat) int {
	total := 0
	for _, s := range stats {
		total += s.txs
	}
	return total
}

// firstError keeps the first error any sender goroutine hit.
type firstError struct {
	mu  sync.Mutex
	err error
}

func (f *firstError) set(err error) {
	f.mu.Lock()
	if f.err == nil {
		f.err = err
	}
	f.mu.Unlock()
}

func (f *firstError) get() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

type rpcBlock struct {
	GasUsed               hexutil.Uint64 `json:"gasUsed"`
	Timestamp             hexutil.Uint64 `json:"timestamp"`
	TimestampMilliseconds hexutil.Uint64 `json:"timestampMilliseconds"`
	Transactions          []common.Hash  `json:"transactions"`
}

// watchBlocks follows the chain head from startBlock until it sees a block
// timestamped past the deadline (plus a drain margin for the mempool tail).
func watchBlocks(ctx context.Context, client *rpc.Client, startBlock uint64, deadline time.Time) ([]blockStat, error) {
	var stats []blockStat
	next := startBlock
	drainUntil := deadline.Add(120 * time.Second)
	for {
		var block *rpcBlock
		err := client.CallContext(ctx, &block, "eth_getBlockByNumber", hexutil.EncodeUint64(next), false)
		if err != nil && !strings.Contains(err.Error(), "unfinalized") {
			return nil, fmt.Errorf("block %d: %w", next, err)
		}
		if err != nil || block == nil {
			if time.Now().After(drainUntil) {
				return stats, nil
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}
			continue
		}
		tsMS := uint64(block.TimestampMilliseconds)
		if tsMS == 0 {
			tsMS = uint64(block.Timestamp) * 1000
		}
		stats = append(stats, blockStat{txs: len(block.Transactions), gasUsed: uint64(block.GasUsed), tsMS: tsMS})
		next++
		if tsMS > uint64(deadline.UnixMilli()) {
			return stats, nil
		}
	}
}
