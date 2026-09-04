// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"fmt"

	"github.com/ava-labs/libevm/eth/filters"
	"github.com/ava-labs/libevm/libevm/debug"
	"github.com/ava-labs/libevm/libevm/ethapi"
	"github.com/ava-labs/libevm/rpc"

	// Force-load tracer engines to trigger registration of the JS and native
	// (e.g. "callTracer") tracers available to debug_trace* APIs.
	_ "github.com/ava-labs/libevm/eth/tracers/js"
	_ "github.com/ava-labs/libevm/eth/tracers/native"

	"github.com/ava-labs/avalanchego/utils/set"
)

// Taken as the default from geth / libevm's `node.DefaultConfig`.
const batchResponseMaxSize = 25 * 1000 * 1000 // 25 MB

// An API is a named group of JSON-RPC methods that a node MAY serve, see
// [Config.APIs]. Groups do not align with JSON-RPC namespaces: for security
// and compatibility reasons, methods within a single namespace must be
// selectable independently, and the namespaces themselves are locked in.
type API string

// Every available [API]. The methods each one carries are listed alongside its
// registration in [apiServices].
const (
	APIWeb3          API = "web3"
	APINet           API = "net"
	APITxPool        API = "txpool"
	APIGas           API = "gas"
	APIChain         API = "chain"
	APITransactions  API = "transactions"
	APISubscriptions API = "subscriptions"
	APIAvalanche     API = "avalanche"
	APIDB            API = "db"
	APIProfile       API = "profile"
	APITrace         API = "trace"
)

// AllAPIs returns every [API].
func AllAPIs() set.Set[API] {
	all := set.NewSet[API](len(apiServices))
	for _, s := range apiServices {
		all.Add(s.name)
	}
	return all
}

// DefaultAPIs returns the [API]s served when an operator doesn't configure
// [Config.APIs] (those with [apiService.defaultOn])).
func DefaultAPIs() set.Set[API] {
	d := set.NewSet[API](len(apiServices))
	for _, s := range apiServices {
		if s.defaultOn {
			d.Add(s.name)
		}
	}
	return d
}

// An apiService constructs the receiver registered for a single [API].
type apiService struct {
	name      API
	namespace string
	defaultOn bool
	receiver  func(b *backend, filter *filters.FilterAPI) any
}

// apiServices is every registerable service. Enabling an [API] registers every
// service with that name.
//
// Standard Ethereum APIs are documented at: https://ethereum.org/developers/docs/apis/json-rpc
// geth-specific APIs are documented at: https://geth.ethereum.org/docs/interacting-with-geth/rpc
var apiServices = []apiService{
	{
		// Standard Ethereum node APIs:
		// - web3_clientVersion
		// - web3_sha3
		name: APIWeb3, namespace: "web3", defaultOn: true,
		receiver: func(*backend, *filters.FilterAPI) any { return newWeb3API() },
	},
	{
		// Standard Ethereum node APIs:
		// - net_listening
		// - net_peerCount
		// - net_version
		name: APINet, namespace: "net", defaultOn: true,
		receiver: func(b *backend, _ *filters.FilterAPI) any {
			return newNetAPI(b.Peers(), b.ChainConfig().ChainID.Uint64())
		},
	},
	{
		// geth-specific APIs:
		// - txpool_content
		// - txpool_contentFrom
		// - txpool_inspect
		// - txpool_status
		name: APITxPool, namespace: "txpool", defaultOn: true,
		receiver: func(b *backend, _ *filters.FilterAPI) any { return ethapi.NewTxPoolAPI(b) },
	},
	{
		// Standard Ethereum node APIs:
		// - eth_gasPrice
		// - eth_maxPriorityFeePerGas
		// - eth_feeHistory
		// - eth_syncing
		name: APIGas, namespace: "eth", defaultOn: true,
		receiver: func(b *backend, _ *filters.FilterAPI) any { return ethapi.NewEthereumAPI(b) },
	},
	{
		// Standard Ethereum node APIs:
		// - eth_blockNumber
		// - eth_call
		// - eth_chainId
		// - eth_estimateGas
		// - eth_getBalance
		// - eth_getBlockByHash
		// - eth_getBlockByNumber
		// - eth_getCode
		// - eth_getProof
		// - eth_getStorageAt
		// - eth_getUncleByBlockHashAndIndex
		// - eth_getUncleByBlockNumberAndIndex
		// - eth_getUncleCountByBlockHash
		// - eth_getUncleCountByBlockNumber
		//
		// Geth-specific APIs:
		// - eth_createAccessList
		// - eth_getHeaderByHash
		// - eth_getHeaderByNumber
		//
		// Undocumented APIs:
		// - eth_getBlockReceipts
		name: APIChain, namespace: "eth", defaultOn: true,
		receiver: func(b *backend, _ *filters.FilterAPI) any {
			return &blockChainAPI{ethapi.NewBlockChainAPI(b), b}
		},
	},
	{
		// Standard Ethereum node APIs:
		// - eth_getBlockTransactionCountByHash
		// - eth_getBlockTransactionCountByNumber
		// - eth_getTransactionByBlockHashAndIndex
		// - eth_getTransactionByBlockNumberAndIndex
		// - eth_getTransactionByHash
		// - eth_getTransactionCount
		// - eth_getTransactionReceipt
		// - eth_sendRawTransaction
		// - eth_sendTransaction
		// - eth_sign
		// - eth_signTransaction
		//
		// Undocumented APIs:
		// - eth_fillTransaction
		// - eth_getRawTransactionByBlockHashAndIndex
		// - eth_getRawTransactionByBlockNumberAndIndex
		// - eth_getRawTransactionByHash
		// - eth_pendingTransactions
		// - eth_resend
		name: APITransactions, namespace: "eth", defaultOn: true,
		receiver: func(b *backend, _ *filters.FilterAPI) any {
			return immediateReceipts{b.RecentReceipt, ethapi.NewTransactionAPI(b, new(ethapi.AddrLocker))}
		},
	},
	{
		// Standard Ethereum node APIS:
		// - eth_getFilterChanges
		// - eth_getFilterLogs
		// - eth_getLogs
		// - eth_newBlockFilter
		// - eth_newFilter
		// - eth_newPendingTransactionFilter
		// - eth_uninstallFilter
		//
		// geth-specific APIs:
		// - eth_subscribe
		//  - newHeads
		//  - newPendingTransactions
		//  - logs
		name: APISubscriptions, namespace: "eth", defaultOn: true,
		receiver: func(_ *backend, filter *filters.FilterAPI) any { return filter },
	},
	{
		// Avalanche-custom eth extensions:
		// - eth_subscribe
		//  - newAcceptedTransactions
		name: APISubscriptions, namespace: "eth", defaultOn: true,
		receiver: func(b *backend, _ *filters.FilterAPI) any { return &customSubscriptionAPI{b} },
	},
	{
		// Avalanche-custom eth extensions:
		// - eth_baseFee
		// - eth_callDetailed
		// - eth_getChainConfig
		// - eth_suggestPriceOptions
		name: APIAvalanche, namespace: "eth", defaultOn: true,
		receiver: func(b *backend, _ *filters.FilterAPI) any { return &customAPI{b} },
	},
	{
		// geth-specific APIs:
		// - debug_chaindbCompact
		// - debug_chaindbProperty
		// - debug_dbAncient
		// - debug_dbAncients
		// - debug_dbGet
		// - debug_getRawBlock
		// - debug_getRawHeader
		// - debug_getRawReceipts
		// - debug_getRawTransaction
		// - debug_printBlock
		// - debug_setHead          (no-op, logs info)
		name: APIDB, namespace: "debug", // raw database access
		receiver: func(b *backend, _ *filters.FilterAPI) any { return ethapi.NewDebugAPI(b) },
	},
	{
		// geth-specific APIs:
		// - debug_blockProfile
		// - debug_cpuProfile
		// - debug_freeOSMemory
		// - debug_gcStats
		// - debug_goTrace
		// - debug_memStats
		// - debug_mutexProfile
		// - debug_setBlockProfileRate
		// - debug_setGCPercent
		// - debug_setMutexProfileFraction
		// - debug_stacks
		// - debug_startCPUProfile
		// - debug_startGoTrace
		// - debug_stopCPUProfile
		// - debug_stopGoTrace
		// - debug_verbosity
		// - debug_vmodule
		// - debug_writeBlockProfile
		// - debug_writeMemProfile
		// - debug_writeMutexProfile
		name: APIProfile, namespace: "debug", // process introspection
		receiver: func(*backend, *filters.FilterAPI) any { return debug.Handler },
	},
	{
		// geth-specific APIs:
		// - debug_intermediateRoots
		// - debug_standardTraceBadBlockToFile
		// - debug_standardTraceBlockToFile
		// - debug_traceBadBlock
		// - debug_traceBlock
		// - debug_traceBlockByHash
		// - debug_traceBlockByNumber
		// - debug_traceBlockFromFile
		// - debug_traceCall
		// - debug_subscribe
		//  - traceChain // TODO(JonathanOppenheimer): test this RPC
		// - debug_traceTransaction
		name: APITrace, namespace: "debug", defaultOn: true,
		receiver: func(b *backend, _ *filters.FilterAPI) any { return newTracerAPI(b) },
	},
}

// Server returns the Provider's [rpc.Server], with the JSON-RPC namespace
// handlers for every enabled [API] registered.
func (p *Provider) Server() *rpc.Server {
	return p.server
}

func (b *backend) server(filter *filters.FilterAPI) (*rpc.Server, error) {
	s := rpc.NewServer()
	s.SetBatchLimits(int(b.config.BatchRequestLimit), batchResponseMaxSize) // #nosec G115 -- [Config.Verify], bounds-checks against math.MaxInt
	for _, svc := range apiServices {
		if !b.config.APIs.Contains(svc.name) {
			continue
		}
		r := svc.receiver(b, filter)
		if err := s.RegisterName(svc.namespace, r); err != nil {
			return nil, fmt.Errorf("%T.RegisterName(%q, %T): %v", s, svc.namespace, r, err)
		}
	}
	return s, nil
}
