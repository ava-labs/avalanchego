// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saevm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/sae"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/evm/utils/rpc"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/evm/database"
	"github.com/ava-labs/avalanchego/vms/saevm/api"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/hook/acp176"
	"github.com/ava-labs/avalanchego/vms/saevm/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/txpool"

	avadb "github.com/ava-labs/avalanchego/database"
	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
	warpcontract "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	avawarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	saewarp "github.com/ava-labs/avalanchego/vms/saevm/warp"
)

// SinceGenesis is a harness around an [sae.VM], providing an `Initialize`
// method that treats the chain as being asynchronous since genesis.
type SinceGenesis struct {
	*sae.VM // created by [SinceGenesis.Initialize]
	config  sae.Config

	ctx          *snow.Context
	db           avadb.Database
	mempool      *txpool.Mempool
	pushGossiper *gossip.PushGossiper[*tx.Tx]

	// TODO(StephenButtolph): Remove. This is only used by the tests.
	warpVerifier *saewarp.Verifier

	// onClose are executed in reverse order during [SinceGenesis.Shutdown].
	// If a resource depends on another resource, it MUST be added AFTER the
	// resource it depends on.
	onClose []func()

	preference       atomic.Pointer[blocks.Block]
	lastWaitForEvent utils.Atomic[time.Time]
}

// NewSinceGenesis constructs a new [SinceGenesis].
func NewSinceGenesis(c sae.Config) *SinceGenesis {
	return &SinceGenesis{
		config: c,
	}
}

type Config struct {
	// GasTarget is the target gas per second that this node will attempt to use
	// when creating blocks. If this config is not specified, the node will
	// default to use the parent block's target gas per second.
	GasTarget *gas.Gas `json:"gas-target,omitempty"`

	// MinDelayTarget is the minimum delay between blocks (in milliseconds) that
	// this node will attempt to use when creating blocks. If this config is not
	// specified, the node will default to use the parent block's target delay
	// per second.
	MinDelayTarget *uint64 `json:"min-delay-target,omitempty"`

	// WarpOffChainMessages encodes off-chain messages (unrelated to any
	// on-chain event ie. block or AddressedCall) that the node should be
	// willing to sign.
	WarpOffChainMessages []hexutil.Bytes `json:"warp-off-chain-messages"`
}

func (c Config) WarpMessages() ([]*avawarp.UnsignedMessage, error) {
	msgs := make([]*avawarp.UnsignedMessage, len(c.WarpOffChainMessages))
	for i, bytes := range c.WarpOffChainMessages {
		msg, err := avawarp.ParseUnsignedMessage(bytes)
		if err != nil {
			return nil, fmt.Errorf("ailed to parse off-chain message at index %d: %w", i, err)
		}
		msgs[i] = msg
	}
	return msgs, nil
}

var ethDBPrefix = []byte("ethdb")

// Initialize initializes the VM.
func (vm *SinceGenesis) Initialize(
	ctx context.Context,
	snowCtx *snow.Context,
	avaDB avadb.Database,
	genesisBytes []byte,
	_ []byte,
	configBytes []byte,
	_ []*common.Fx,
	appSender common.AppSender,
) error {
	// [prefixdb.NewNested] is used because coreth used to be run as a plugin.
	// This meant that the database's prefix was not compacted, because the
	// provided database was wrapped by the rpcchainvm.
	db := rawdb.NewDatabase(database.New(prefixdb.NewNested(ethDBPrefix, avaDB)))
	tdb := triedb.NewDatabase(db, vm.config.DBConfig.TrieDBConfig)

	genesis := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, genesis); err != nil {
		return fmt.Errorf("json.Unmarshal(%T): %w", genesis, err)
	}

	{
		c := genesis.Config
		u := snowCtx.NetworkUpgrades

		c.HomesteadBlock = big.NewInt(0)
		c.DAOForkBlock = big.NewInt(0)
		c.DAOForkSupport = true
		c.EIP150Block = big.NewInt(0)
		c.EIP155Block = big.NewInt(0)
		c.EIP158Block = big.NewInt(0)
		c.ByzantiumBlock = big.NewInt(0)
		c.ConstantinopleBlock = big.NewInt(0)
		c.PetersburgBlock = big.NewInt(0)
		c.IstanbulBlock = big.NewInt(0)
		c.MuirGlacierBlock = big.NewInt(0)
		c.BerlinBlock = big.NewInt(0)
		c.LondonBlock = big.NewInt(0)
		c.ShanghaiTime = utils.PointerTo(uint64(u.DurangoTime.Unix()))
		c.CancunTime = utils.PointerTo(uint64(u.EtnaTime.Unix()))

		chainConfigExtra := &extras.ChainConfig{
			NetworkUpgrades: extras.GetNetworkUpgrades(u),
			AvalancheContext: extras.AvalancheContext{
				SnowCtx: snowCtx,
			},
		}
		if chainConfigExtra.DurangoBlockTimestamp != nil {
			chainConfigExtra.PrecompileUpgrades = append(chainConfigExtra.PrecompileUpgrades, extras.PrecompileUpgrade{
				Config: warpcontract.NewDefaultConfig(chainConfigExtra.DurangoBlockTimestamp),
			})
		}
		corethparams.WithExtra(c, chainConfigExtra)
	}

	config, _, err := core.SetupGenesisBlock(db, tdb, genesis)
	if err != nil {
		return fmt.Errorf("core.SetupGenesisBlock(...): %w", err)
	}

	var userConfig Config
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &userConfig); err != nil {
			return fmt.Errorf("json.Unmarshal(%T): %w", userConfig, err)
		}
	}

	warpMessages, err := userConfig.WarpMessages()
	if err != nil {
		return err
	}

	var desiredDelayExcess *acp226.DelayExcess
	if userConfig.MinDelayTarget != nil {
		desiredDelayExcess = new(acp226.DelayExcess)
		*desiredDelayExcess = acp226.DesiredDelayExcess(*userConfig.MinDelayTarget)
	}
	var desiredTargetExcess *acp176.TargetExcess
	if userConfig.GasTarget != nil {
		desiredTargetExcess = new(acp176.TargetExcess)
		*desiredTargetExcess = acp176.DesiredTargetExcess(*userConfig.GasTarget)
	}

	txs := txpool.NewTxs()
	warpStorage := saewarp.NewStorage(avaDB, warpMessages...)
	hooks := hook.NewPoints(
		snowCtx,
		avaDB,
		config,
		desiredDelayExcess,
		desiredTargetExcess,
		txs,
		warpStorage,
	)
	inner, err := sae.NewVM(ctx, hooks, vm.config, snowCtx, config, db, genesis.ToBlock(), appSender)
	if err != nil {
		return err
	}
	vm.VM = inner
	vm.ctx = snowCtx
	vm.db = avaDB
	vm.mempool = txpool.New(txs, snowCtx, inner.GethRPCBackends())
	vm.onClose = append(vm.onClose, vm.mempool.Close)

	metrics := prometheus.NewRegistry()
	if err := snowCtx.Metrics.Register("coreth", metrics); err != nil {
		return fmt.Errorf("failed to register metrics: %w", err)
	}

	{ // ==========  P2P Gossip  ==========
		gossipSet, err := gossip.NewBloomSet(vm.mempool, gossip.BloomSetConfig{})
		if err != nil {
			return fmt.Errorf("failed to create bloom set: %w", err)
		}

		const pullGossipPeriod = time.Second
		handler, pullGossiper, pushGossiper, err := gossip.NewSystem(
			snowCtx.NodeID,
			vm.Network,
			vm.ValidatorPeers,
			gossipSet,
			tx.Marshaller{},
			gossip.SystemConfig{
				Log:           snowCtx.Log,
				Registry:      metrics,
				Namespace:     "gossip",
				HandlerID:     p2p.AtomicTxGossipHandlerID,
				RequestPeriod: pullGossipPeriod,
			},
		)
		if err != nil {
			return fmt.Errorf("failed to initialize atomic gossip system: %w", err)
		}
		vm.pushGossiper = pushGossiper

		if err := inner.AddHandler(p2p.AtomicTxGossipHandlerID, handler); err != nil {
			return fmt.Errorf("network.AddHandler(atomic): %w", err)
		}

		var (
			gossipCtx, cancel = context.WithCancel(context.Background())
			wg                sync.WaitGroup
		)
		wg.Go(func() {
			gossip.Every(gossipCtx, snowCtx.Log, pullGossiper, pullGossipPeriod)
		})
		wg.Go(func() {
			const pushGossipPeriod = 100 * time.Millisecond
			gossip.Every(gossipCtx, snowCtx.Log, pushGossiper, pushGossipPeriod)
		})
		vm.onClose = append(vm.onClose, func() {
			cancel()
			wg.Wait()
		})
	}

	{ // ==========  Warp Handler  ==========
		vm.warpVerifier = saewarp.NewVerifier(&blockClient{vm: inner}, warpStorage)
		warpHandler := acp118.NewHandler(vm.warpVerifier, snowCtx.WarpSigner)
		if err := inner.AddHandler(p2p.SignatureRequestHandlerID, warpHandler); err != nil {
			return fmt.Errorf("network.AddHandler(warp): %w", err)
		}
	}

	return nil
}

const (
	avaxServiceName       = "avax"
	avaxHTTPExtensionPath = "/" + avaxServiceName
)

func (vm *SinceGenesis) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	m, err := vm.VM.CreateHandlers(ctx)
	if err != nil {
		return nil, err
	}

	service := api.NewService(vm.ctx, vm.GethRPCBackends(), vm.mempool, vm.pushGossiper, vm.db)
	handler, err := rpc.NewHandler(avaxServiceName, service)
	if err != nil {
		return nil, fmt.Errorf("rpc.NewHandler(%s, ...): %w", avaxServiceName, err)
	}

	m[avaxHTTPExtensionPath] = handler
	return m, nil
}

func (vm *SinceGenesis) SetPreference(ctx context.Context, id ids.ID, bCtx *block.Context) error {
	b, err := vm.GetBlock(ctx, id)
	if err != nil {
		return err
	}
	vm.preference.Store(b)
	return vm.VM.SetPreference(ctx, id, bCtx)
}

// Prevent busy looping when the chain is more advanced than the mempool.
const waitForEventDelay = 100 * time.Millisecond

var errNoPreference = errors.New("no preferred block")

// WaitForEvent waits for the next event from the VM.
func (vm *SinceGenesis) WaitForEvent(ctx context.Context) (common.Message, error) {
	// Avoid busy looping if we seem like we are ready to build a block, but are
	// encountering an error.
	{
		defer func() {
			vm.lastWaitForEvent.Set(time.Now())
		}()

		sinceLastCall := time.Since(vm.lastWaitForEvent.Get())
		timeToWait := waitForEventDelay - sinceLastCall
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(timeToWait):
		}
	}

	// Wait until we are allowed to build a block.
	{
		parent := vm.preference.Load()
		if parent == nil {
			return 0, errNoPreference
		}

		minTime := minNextBlockTime(parent.Header())
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(time.Until(minTime)):
		}
	}

	// Wait until we want to build a block.
	ctx, cancel := context.WithCancel(ctx)
	type result struct {
		msg common.Message
		err error
	}
	results := make(chan result, 2)
	go func() {
		defer cancel()
		msg, err := vm.VM.WaitForEvent(ctx)
		results <- result{msg, err}
	}()
	go func() {
		defer cancel()
		err := vm.mempool.Txs.AwaitTxs(ctx)
		results <- result{common.PendingTxs, err}
	}()

	r := <-results
	return r.msg, r.err
}

// minNextBlockTime calculates the minimum next block time based on the header.
func minNextBlockTime(h *types.Header) time.Time {
	e := customtypes.GetHeaderExtra(h)
	// If the parent header has no min delay excess, there is nothing to wait
	// for, because the rule does not apply to the block to be built.
	if e.MinDelayExcess == nil {
		return time.Time{}
	}

	mde := *e.MinDelayExcess
	// delay excess is already verified by consensus so this can not overflow.
	delay := time.Duration(mde.Delay()) * time.Millisecond
	return customtypes.BlockTime(h).Add(delay)
}

func (vm *SinceGenesis) RejectBlock(ctx context.Context, b *blocks.Block) error {
	// If the block is rejected, the transactions might get dropped from the
	// network. If the transactions are still valid, it is a better UX to add
	// them into our mempool.
	txs, err := tx.ParseSlice(customtypes.BlockExtData(b.EthBlock()))
	if err != nil {
		return fmt.Errorf("failed to extract txs of block %s (%d): %w", b.Hash(), b.NumberU64(), err)
	}
	for _, tx := range txs {
		_ = vm.mempool.Add(tx)
	}
	return vm.VM.RejectBlock(ctx, b)
}

func (vm *SinceGenesis) Shutdown(ctx context.Context) error {
	for _, f := range slices.Backward(vm.onClose) {
		f()
	}
	if vm.VM == nil {
		return nil
	}
	return vm.VM.Shutdown(ctx)
}

// blockClient adapts [sae.VM] to the [saewarp.BlockClient] interface.
type blockClient struct {
	vm *sae.VM
}

var _ saewarp.BlockClient = (*blockClient)(nil)

func (c *blockClient) IsAccepted(ctx context.Context, blockID ids.ID) error {
	b, err := c.vm.GetBlock(ctx, blockID)
	if err != nil {
		return err
	}
	acceptedID, err := c.vm.GetBlockIDAtHeight(ctx, b.Height())
	if err != nil {
		return err
	}
	if acceptedID != blockID {
		return avadb.ErrNotFound
	}
	return nil
}
