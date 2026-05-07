// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"fmt"

	"github.com/ava-labs/libevm/eth/filters"
	"github.com/ava-labs/libevm/eth/tracers"
	"github.com/ava-labs/libevm/libevm/debug"
	"github.com/ava-labs/libevm/libevm/ethapi"
	"github.com/ava-labs/libevm/rpc"
)

// Taken as the defaults from geth / libevm's `node.DefaultConfig`.
const (
	batchLimit           = 1000
	batchResponseMaxSize = 25 * 1000 * 1000 // 25 MB
)

// Server returns the Provider's [rpc.Server], with all configured JSON-RPC
// namespace handlers registered.
func (p *Provider) Server() *rpc.Server {
	return p.server
}

func (b *backend) server(filter *filters.FilterAPI) (*rpc.Server, error) {
	type api struct {
		namespace string
		api       any
	}

	// Standard Ethereum APIs are documented at: https://ethereum.org/developers/docs/apis/json-rpc
	// geth-specific APIs are documented at: https://geth.ethereum.org/docs/interacting-with-geth/rpc
	apis := []api{
		// Standard Ethereum node APIs:
		// - web3_clientVersion
		// - web3_sha3
		{"web3", newWeb3API()},
		// Standard Ethereum node APIs:
		// - net_listening
		// - net_peerCount
		// - net_version
		{"net", newNetAPI(b.Peers(), b.ChainConfig().ChainID.Uint64())},
		// geth-specific APIs:
		// - txpool_content
		// - txpool_contentFrom
		// - txpool_inspect
		// - txpool_status
		{"txpool", ethapi.NewTxPoolAPI(b)},
		// Standard Ethereum node APIs:
		// - eth_gasPrice
		// - eth_maxPriorityFeePerGas
		// - eth_feeHistory
		// - eth_syncing
		{"eth", ethapi.NewEthereumAPI(b)},
		// Standard Ethereum node APIs:
		// - eth_blockNumber
		// - eth_chainId
		// - eth_getBlockByHash
		// - eth_getBlockByNumber
		// - eth_getBlockReceipts
		// - eth_getUncleByBlockHashAndIndex
		// - eth_getUncleByBlockNumberAndIndex
		// - eth_getUncleCountByBlockHash
		// - eth_getUncleCountByBlockNumber
		//
		// geth-specific APIs:
		// - eth_getHeaderByHash
		// - eth_getHeaderByNumber
		{"eth", &blockChainAPI{ethapi.NewBlockChainAPI(b), b}},
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
		{
			"eth",
			immediateReceipts{
				b.RecentReceipt,
				ethapi.NewTransactionAPI(b, new(ethapi.AddrLocker)),
			},
		},
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
		{"eth", filter},
		// Avalanche-custom eth extensions:
		{"eth", &customAPI{b}},
	}

	if b.config.EnableDBInspecting {
		apis = append(apis, api{
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
			"debug", ethapi.NewDebugAPI(b),
		})
	}

	if b.config.EnableProfiling {
		apis = append(apis, api{
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
			"debug", debug.Handler,
		})
	}

	if !b.config.DisableTracing {
		apis = append(apis, api{
			// geth-specific APIs:
			"debug", tracers.NewAPI(b),
		})
	}

	s := rpc.NewServer()
	s.SetBatchLimits(batchLimit, batchResponseMaxSize)
	for _, api := range apis {
		if err := s.RegisterName(api.namespace, api.api); err != nil {
			return nil, fmt.Errorf("%T.RegisterName(%q, %T): %v", s, api.namespace, api.api, err)
		}
	}
	return s, nil
}
