// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package vm

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ava-labs/coreth/plugin/evm/atomic"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/coreth/plugin/evm/client"
	"github.com/ava-labs/libevm/log"
)

const (
	// Max number of addresses that can be passed in as argument to GetUTXOs
	maxGetUTXOsAddrs = 1024
	maxUTXOsToFetch  = 1024
)

var (
	errNoAddresses   = errors.New("no addresses provided")
	errNoSourceChain = errors.New("no source chain provided")
	errNilTxID       = errors.New("nil transaction ID")
)

// AvaxAPI offers Avalanche network related API methods
type AvaxAPI struct{ vm *VM }

type VersionReply struct {
	Version string `json:"version"`
}

// ClientVersion returns the version of the VM running
func (service *AvaxAPI) Version(r *http.Request, _ *struct{}, reply *VersionReply) error {
	version, err := service.vm.InnerVM.Version(context.Background())
	if err != nil {
		return err
	}
	reply.Version = version
	return nil
}

// GetUTXOs gets all utxos for passed in addresses
func (service *AvaxAPI) GetUTXOs(r *http.Request, args *api.GetUTXOsArgs, reply *api.GetUTXOsReply) error {
	log.Info("EVM: GetUTXOs called", "Addresses", args.Addresses)

	if len(args.Addresses) == 0 {
		return errNoAddresses
	}
	if len(args.Addresses) > maxGetUTXOsAddrs {
		return fmt.Errorf("number of addresses given, %d, exceeds maximum, %d", len(args.Addresses), maxGetUTXOsAddrs)
	}

	if args.SourceChain == "" {
		return errNoSourceChain
	}

	sourceChainID, err := service.vm.Ctx.BCLookup.Lookup(args.SourceChain)
	if err != nil {
		return fmt.Errorf("problem parsing source chainID %q: %w", args.SourceChain, err)
	}

	addrSet := set.Set[ids.ShortID]{}
	for _, addrStr := range args.Addresses {
		addr, err := ParseServiceAddress(service.vm.Ctx, addrStr)
		if err != nil {
			return fmt.Errorf("couldn't parse address %q: %w", addrStr, err)
		}
		addrSet.Add(addr)
	}

	startAddr := ids.ShortEmpty
	startUTXO := ids.Empty
	if args.StartIndex.Address != "" || args.StartIndex.UTXO != "" {
		startAddr, err = ParseServiceAddress(service.vm.Ctx, args.StartIndex.Address)
		if err != nil {
			return fmt.Errorf("couldn't parse start index address %q: %w", args.StartIndex.Address, err)
		}
		startUTXO, err = ids.FromString(args.StartIndex.UTXO)
		if err != nil {
			return fmt.Errorf("couldn't parse start index utxo: %w", err)
		}
	}

	service.vm.Ctx.Lock.Lock()
	defer service.vm.Ctx.Lock.Unlock()

	limit := int(args.Limit)

	if limit <= 0 || limit > maxUTXOsToFetch {
		limit = maxUTXOsToFetch
	}

	utxos, endAddr, endUTXOID, err := avax.GetAtomicUTXOs(
		service.vm.Ctx.SharedMemory,
		atomic.Codec,
		sourceChainID,
		addrSet,
		startAddr,
		startUTXO,
		limit,
	)
	if err != nil {
		return fmt.Errorf("problem retrieving UTXOs: %w", err)
	}

	reply.UTXOs = make([]string, len(utxos))
	for i, utxo := range utxos {
		b, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
		if err != nil {
			return fmt.Errorf("problem marshalling UTXO: %w", err)
		}
		str, err := formatting.Encode(args.Encoding, b)
		if err != nil {
			return fmt.Errorf("problem encoding utxo: %w", err)
		}
		reply.UTXOs[i] = str
	}

	endAddress, err := FormatLocalAddress(service.vm.Ctx, endAddr)
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}

	reply.EndIndex.Address = endAddress
	reply.EndIndex.UTXO = endUTXOID.String()
	reply.NumFetched = json.Uint64(len(utxos))
	reply.Encoding = args.Encoding
	return nil
}

func (service *AvaxAPI) IssueTx(r *http.Request, args *api.FormattedTx, response *api.JSONTxID) error {
	log.Info("EVM: IssueTx called")

	txBytes, err := formatting.Decode(args.Encoding, args.Tx)
	if err != nil {
		return fmt.Errorf("problem decoding transaction: %w", err)
	}

	tx := &atomic.Tx{}
	if _, err := atomic.Codec.Unmarshal(txBytes, tx); err != nil {
		return fmt.Errorf("problem parsing transaction: %w", err)
	}
	if err := tx.Sign(atomic.Codec, nil); err != nil {
		return fmt.Errorf("problem initializing transaction: %w", err)
	}

	response.TxID = tx.ID()

	service.vm.Ctx.Lock.Lock()
	defer service.vm.Ctx.Lock.Unlock()

	if err := service.vm.AtomicMempool.AddLocalTx(tx); err != nil {
		return err
	}
	service.vm.AtomicTxPushGossiper.Add(tx)
	return nil
}

// GetAtomicTxStatus returns the status of the specified transaction
func (service *AvaxAPI) GetAtomicTxStatus(r *http.Request, args *api.JSONTxID, reply *client.GetAtomicTxStatusReply) error {
	log.Info("EVM: GetAtomicTxStatus called", "txID", args.TxID)

	if args.TxID == ids.Empty {
		return errNilTxID
	}

	service.vm.Ctx.Lock.Lock()
	defer service.vm.Ctx.Lock.Unlock()

	_, status, height, _ := service.vm.GetAtomicTx(args.TxID)

	reply.Status = status
	if status == atomic.Accepted {
		// Since chain state updates run asynchronously with VM block acceptance,
		// avoid returning [Accepted] until the chain state reaches the block
		// containing the atomic tx.
		lastAccepted := service.vm.InnerVM.Blockchain().LastAcceptedBlock()
		if height > lastAccepted.NumberU64() {
			reply.Status = atomic.Processing
			return nil
		}

		jsonHeight := json.Uint64(height)
		reply.BlockHeight = &jsonHeight
	}
	return nil
}

type FormattedTx struct {
	api.FormattedTx
	BlockHeight *json.Uint64 `json:"blockHeight,omitempty"`
}

// GetAtomicTx returns the specified transaction
func (service *AvaxAPI) GetAtomicTx(r *http.Request, args *api.GetTxArgs, reply *FormattedTx) error {
	log.Info("EVM: GetAtomicTx called", "txID", args.TxID)

	if args.TxID == ids.Empty {
		return errNilTxID
	}

	service.vm.Ctx.Lock.Lock()
	defer service.vm.Ctx.Lock.Unlock()

	tx, status, height, err := service.vm.GetAtomicTx(args.TxID)
	if err != nil {
		return err
	}

	if status == atomic.Unknown {
		return fmt.Errorf("could not find tx %s", args.TxID)
	}

	txBytes, err := formatting.Encode(args.Encoding, tx.SignedBytes())
	if err != nil {
		return err
	}
	reply.Tx = txBytes
	reply.Encoding = args.Encoding
	if status == atomic.Accepted {
		// Since chain state updates run asynchronously with VM block acceptance,
		// avoid returning [Accepted] until the chain state reaches the block
		// containing the atomic tx.
		lastAccepted := service.vm.InnerVM.Blockchain().LastAcceptedBlock()
		if height > lastAccepted.NumberU64() {
			return nil
		}

		jsonHeight := json.Uint64(height)
		reply.BlockHeight = &jsonHeight
	}
	return nil
}
