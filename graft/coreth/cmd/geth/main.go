// Copyright 2014 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

// geth is the official command-line client for Ethereum.
package geth

import (
	"fmt"
	"math"
	"os"
	"runtime"
	godebug "runtime/debug"
	"sort"
	"strconv"
	"time"

	"github.com/elastic/gosigar"
	"github.com/ava-labs/coreth/cmd/utils"
	"github.com/ava-labs/go-ethereum/common"
	"github.com/ava-labs/go-ethereum/console"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/go-ethereum/eth/downloader"
	"github.com/ava-labs/go-ethereum/ethclient"
	"github.com/ava-labs/coreth/internal/debug"
	"github.com/ava-labs/go-ethereum/les"
	"github.com/ava-labs/go-ethereum/log"
	"github.com/ava-labs/go-ethereum/metrics"
	"github.com/ava-labs/coreth/node"
	cli "gopkg.in/urfave/cli.v1"
)

const (
	clientIdentifier = "geth" // Client identifier to advertise over the network
)

var (
	// Git SHA1 commit hash of the release (set via linker flags)
	gitCommit = ""
	gitDate   = ""
	// The App that holds all commands and flags.
	App = utils.NewApp(gitCommit, gitDate, "the go-ethereum command line interface")
	// flags that configure the node
	nodeFlags = []cli.Flag{
		utils.IdentityFlag,
		utils.PasswordFileFlag,
		utils.BootnodesFlag,
		utils.BootnodesV4Flag,
		utils.BootnodesV5Flag,
		utils.DataDirFlag,
		utils.AncientFlag,
		utils.KeyStoreDirFlag,
		utils.ExternalSignerFlag,
		utils.NoUSBFlag,
		utils.SmartCardDaemonPathFlag,
		utils.DashboardEnabledFlag,
		utils.DashboardAddrFlag,
		utils.DashboardPortFlag,
		utils.DashboardRefreshFlag,
		utils.EthashCacheDirFlag,
		utils.EthashCachesInMemoryFlag,
		utils.EthashCachesOnDiskFlag,
		utils.EthashDatasetDirFlag,
		utils.EthashDatasetsInMemoryFlag,
		utils.EthashDatasetsOnDiskFlag,
		utils.TxPoolLocalsFlag,
		utils.TxPoolNoLocalsFlag,
		utils.TxPoolJournalFlag,
		utils.TxPoolRejournalFlag,
		utils.TxPoolPriceLimitFlag,
		utils.TxPoolPriceBumpFlag,
		utils.TxPoolAccountSlotsFlag,
		utils.TxPoolGlobalSlotsFlag,
		utils.TxPoolAccountQueueFlag,
		utils.TxPoolGlobalQueueFlag,
		utils.TxPoolLifetimeFlag,
		utils.SyncModeFlag,
		utils.ExitWhenSyncedFlag,
		utils.GCModeFlag,
		utils.LightServeFlag,
		utils.LightLegacyServFlag,
		utils.LightIngressFlag,
		utils.LightEgressFlag,
		utils.LightMaxPeersFlag,
		utils.LightLegacyPeersFlag,
		utils.LightKDFFlag,
		utils.UltraLightServersFlag,
		utils.UltraLightFractionFlag,
		utils.UltraLightOnlyAnnounceFlag,
		utils.WhitelistFlag,
		utils.CacheFlag,
		utils.CacheDatabaseFlag,
		utils.CacheTrieFlag,
		utils.CacheGCFlag,
		utils.CacheNoPrefetchFlag,
		utils.ListenPortFlag,
		utils.MaxPeersFlag,
		utils.MaxPendingPeersFlag,
		utils.MiningEnabledFlag,
		utils.MinerThreadsFlag,
		utils.MinerLegacyThreadsFlag,
		utils.MinerNotifyFlag,
		utils.MinerGasTargetFlag,
		utils.MinerLegacyGasTargetFlag,
		utils.MinerGasLimitFlag,
		utils.MinerGasPriceFlag,
		utils.MinerLegacyGasPriceFlag,
		utils.MinerEtherbaseFlag,
		utils.MinerLegacyEtherbaseFlag,
		utils.MinerExtraDataFlag,
		utils.MinerLegacyExtraDataFlag,
		utils.MinerRecommitIntervalFlag,
		utils.MinerNoVerfiyFlag,
		utils.NATFlag,
		utils.NoDiscoverFlag,
		utils.DiscoveryV5Flag,
		utils.NetrestrictFlag,
		utils.NodeKeyFileFlag,
		utils.NodeKeyHexFlag,
		utils.DeveloperFlag,
		utils.DeveloperPeriodFlag,
		utils.TestnetFlag,
		utils.RinkebyFlag,
		utils.GoerliFlag,
		utils.VMEnableDebugFlag,
		utils.NetworkIdFlag,
		utils.EthStatsURLFlag,
		utils.FakePoWFlag,
		utils.NoCompactionFlag,
		utils.GpoBlocksFlag,
		utils.GpoPercentileFlag,
		utils.EWASMInterpreterFlag,
		utils.EVMInterpreterFlag,
		configFileFlag,
	}

	rpcFlags = []cli.Flag{
		utils.RPCEnabledFlag,
		utils.RPCListenAddrFlag,
		utils.RPCPortFlag,
		utils.RPCCORSDomainFlag,
		utils.RPCVirtualHostsFlag,
		utils.GraphQLEnabledFlag,
		utils.GraphQLListenAddrFlag,
		utils.GraphQLPortFlag,
		utils.GraphQLCORSDomainFlag,
		utils.GraphQLVirtualHostsFlag,
		utils.RPCApiFlag,
		utils.WSEnabledFlag,
		utils.WSListenAddrFlag,
		utils.WSPortFlag,
		utils.WSApiFlag,
		utils.WSAllowedOriginsFlag,
		utils.IPCDisabledFlag,
		utils.IPCPathFlag,
		utils.InsecureUnlockAllowedFlag,
		utils.RPCGlobalGasCap,
	}

	whisperFlags = []cli.Flag{
		utils.WhisperEnabledFlag,
		utils.WhisperMaxMessageSizeFlag,
		utils.WhisperMinPOWFlag,
		utils.WhisperRestrictConnectionBetweenLightClientsFlag,
	}

	metricsFlags = []cli.Flag{
		utils.MetricsEnabledFlag,
		utils.MetricsEnabledExpensiveFlag,
		utils.MetricsEnableInfluxDBFlag,
		utils.MetricsInfluxDBEndpointFlag,
		utils.MetricsInfluxDBDatabaseFlag,
		utils.MetricsInfluxDBUsernameFlag,
		utils.MetricsInfluxDBPasswordFlag,
		utils.MetricsInfluxDBTagsFlag,
	}
)

func init() {
	// Initialize the CLI App and start Geth
	App.Action = geth
	App.HideVersion = true // we have a command to print the version
	App.Copyright = "Copyright 2013-2019 The go-ethereum Authors"
	App.Commands = []cli.Command{
		// See chaincmd.go:
		initCommand,
		importCommand,
		exportCommand,
		importPreimagesCommand,
		exportPreimagesCommand,
		copydbCommand,
		removedbCommand,
		dumpCommand,
		inspectCommand,
		// See accountcmd.go:
		// See consolecmd.go:
		consoleCommand,
		attachCommand,
		javascriptCommand,
		// See misccmd.go:
		makecacheCommand,
		makedagCommand,
		versionCommand,
		licenseCommand,
		// See config.go
		dumpConfigCommand,
		// See retesteth.go
		retestethCommand,
	}
	sort.Sort(cli.CommandsByName(App.Commands))

	App.Flags = append(App.Flags, nodeFlags...)
	App.Flags = append(App.Flags, rpcFlags...)
	App.Flags = append(App.Flags, consoleFlags...)
	App.Flags = append(App.Flags, debug.Flags...)
	App.Flags = append(App.Flags, whisperFlags...)
	App.Flags = append(App.Flags, metricsFlags...)

	App.Before = func(ctx *cli.Context) error {
		logdir := ""
		if ctx.GlobalBool(utils.DashboardEnabledFlag.Name) {
			logdir = (&node.Config{DataDir: utils.MakeDataDir(ctx)}).ResolvePath("logs")
		}
		if err := debug.Setup(ctx, logdir); err != nil {
			return err
		}
		// If we're a full node on mainnet without --cache specified, bump default cache allowance
		if ctx.GlobalString(utils.SyncModeFlag.Name) != "light" && !ctx.GlobalIsSet(utils.CacheFlag.Name) && !ctx.GlobalIsSet(utils.NetworkIdFlag.Name) {
			// Make sure we're not on any supported preconfigured testnet either
			if !ctx.GlobalIsSet(utils.TestnetFlag.Name) && !ctx.GlobalIsSet(utils.RinkebyFlag.Name) && !ctx.GlobalIsSet(utils.GoerliFlag.Name) && !ctx.GlobalIsSet(utils.DeveloperFlag.Name) {
				// Nope, we're really on mainnet. Bump that cache up!
				log.Info("Bumping default cache on mainnet", "provided", ctx.GlobalInt(utils.CacheFlag.Name), "updated", 4096)
				ctx.GlobalSet(utils.CacheFlag.Name, strconv.Itoa(4096))
			}
		}
		// If we're running a light client on any network, drop the cache to some meaningfully low amount
		if ctx.GlobalString(utils.SyncModeFlag.Name) == "light" && !ctx.GlobalIsSet(utils.CacheFlag.Name) {
			log.Info("Dropping default light client cache", "provided", ctx.GlobalInt(utils.CacheFlag.Name), "updated", 128)
			ctx.GlobalSet(utils.CacheFlag.Name, strconv.Itoa(128))
		}
		// Cap the cache allowance and tune the garbage collector
		var mem gosigar.Mem
		// Workaround until OpenBSD support lands into gosigar
		// Check https://github.com/elastic/gosigar#supported-platforms
		if runtime.GOOS != "openbsd" {
			if err := mem.Get(); err == nil {
				allowance := int(mem.Total / 1024 / 1024 / 3)
				if cache := ctx.GlobalInt(utils.CacheFlag.Name); cache > allowance {
					log.Warn("Sanitizing cache to Go's GC limits", "provided", cache, "updated", allowance)
					ctx.GlobalSet(utils.CacheFlag.Name, strconv.Itoa(allowance))
				}
			}
		}
		// Ensure Go's GC ignores the database cache for trigger percentage
		cache := ctx.GlobalInt(utils.CacheFlag.Name)
		gogc := math.Max(20, math.Min(100, 100/(float64(cache)/1024)))

		log.Debug("Sanitizing Go's GC trigger", "percent", int(gogc))
		godebug.SetGCPercent(int(gogc))

		// Start metrics export if enabled
		utils.SetupMetrics(ctx)

		// Start system runtime metrics collection
		go metrics.CollectProcessMetrics(3 * time.Second)

		return nil
	}

	App.After = func(ctx *cli.Context) error {
		debug.Exit()
		console.Stdin.Close() // Resets terminal mode.
		return nil
	}
}

func main() {
	if err := App.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// geth is the main entry point into the system if no special subcommand is ran.
// It creates a default node based on the command line arguments and runs it in
// blocking mode, waiting for it to be shut down.
func geth(ctx *cli.Context) error {
	if args := ctx.Args(); len(args) > 0 {
		return fmt.Errorf("invalid command: %q", args[0])
	}
	node := makeFullNode(ctx)
	defer node.Close()
	startNode(ctx, node)
	node.Wait()
	return nil
}

// startNode boots up the system node and all registered protocols, after which
// it unlocks any requested accounts, and starts the RPC/IPC interfaces and the
// miner.
func startNode(ctx *cli.Context, stack *node.Node) {
	debug.Memsize.Add("node", stack)

	// Start up the node itself
	utils.StartNode(stack)

	// Unlock any account specifically requested

	// Register wallet event handlers to open and auto-derive wallets

	// Create a client to interact with local geth node.
	rpcClient, err := stack.Attach()
	if err != nil {
		utils.Fatalf("Failed to attach to self: %v", err)
	}
	ethClient := ethclient.NewClient(rpcClient)

	// Set contract backend for ethereum service if local node
	// is serving LES requests.
	if ctx.GlobalInt(utils.LightLegacyServFlag.Name) > 0 || ctx.GlobalInt(utils.LightServeFlag.Name) > 0 {
		var ethService *eth.Ethereum
		if err := stack.Service(&ethService); err != nil {
			utils.Fatalf("Failed to retrieve ethereum service: %v", err)
		}
		ethService.SetContractBackend(ethClient)
	}
	// Set contract backend for les service if local node is
	// running as a light client.
	if ctx.GlobalString(utils.SyncModeFlag.Name) == "light" {
		var lesService *les.LightEthereum
		if err := stack.Service(&lesService); err != nil {
			utils.Fatalf("Failed to retrieve light ethereum service: %v", err)
		}
		lesService.SetContractBackend(ethClient)
	}

	// Spawn a standalone goroutine for status synchronization monitoring,
	// close the node when synchronization is complete if user required.
	if ctx.GlobalBool(utils.ExitWhenSyncedFlag.Name) {
		go func() {
			sub := stack.EventMux().Subscribe(downloader.DoneEvent{})
			defer sub.Unsubscribe()
			for {
				event := <-sub.Chan()
				if event == nil {
					continue
				}
				done, ok := event.Data.(downloader.DoneEvent)
				if !ok {
					continue
				}
				if timestamp := time.Unix(int64(done.Latest.Time), 0); time.Since(timestamp) < 10*time.Minute {
					log.Info("Synchronisation completed", "latestnum", done.Latest.Number, "latesthash", done.Latest.Hash(),
						"age", common.PrettyAge(timestamp))
					stack.Stop()
				}
			}
		}()
	}

	// Start auxiliary services if enabled
	if ctx.GlobalBool(utils.MiningEnabledFlag.Name) || ctx.GlobalBool(utils.DeveloperFlag.Name) {
		// Mining only makes sense if a full Ethereum node is running
		if ctx.GlobalString(utils.SyncModeFlag.Name) == "light" {
			utils.Fatalf("Light clients do not support mining")
		}
		var ethereum *eth.Ethereum
		if err := stack.Service(&ethereum); err != nil {
			utils.Fatalf("Ethereum service not running: %v", err)
		}
        etherBase := &common.Address {
            1, 0, 0, 0, 0, 0, 0, 0, 0, 0,
            0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
        }
        ethereum.SetEtherbase(*etherBase)
		// Set the gas price to the limits from the CLI and start mining
		gasprice := utils.GlobalBig(ctx, utils.MinerLegacyGasPriceFlag.Name)
		if ctx.IsSet(utils.MinerGasPriceFlag.Name) {
			gasprice = utils.GlobalBig(ctx, utils.MinerGasPriceFlag.Name)
		}
		ethereum.TxPool().SetGasPrice(gasprice)

		threads := ctx.GlobalInt(utils.MinerLegacyThreadsFlag.Name)
		if ctx.GlobalIsSet(utils.MinerThreadsFlag.Name) {
			threads = ctx.GlobalInt(utils.MinerThreadsFlag.Name)
		}
		if err := ethereum.StartMining(threads); err != nil {
			utils.Fatalf("Failed to start mining: %v", err)
		}
	}
}
