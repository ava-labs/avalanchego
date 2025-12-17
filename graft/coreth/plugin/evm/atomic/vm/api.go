// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"errors"
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/txpool"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/client"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"

	atomicstate "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"
	avalanchegossip "github.com/ava-labs/avalanchego/network/p2p/gossip"
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
type AvaxAPI struct {
	// If non-nil, bc is used to prevent returning any transactions as accepted
	// prior to having advanced the chain state to the block containing the
	// transaction.
	bc *core.BlockChain

	Context      *snow.Context
	Mempool      *txpool.Mempool
	PushGossiper *avalanchegossip.PushGossiper[*atomic.Tx]
	AcceptedTxs  *atomicstate.AtomicRepository
}

// GetUTXOs gets all utxos for passed in addresses
func (service *AvaxAPI) GetUTXOs(_ *http.Request, args *api.GetUTXOsArgs, reply *api.GetUTXOsReply) error {
	service.Context.Log.Debug("API called",
		zap.String("service", "avax"),
		zap.String("method", "getUTXOs"),
		logging.UserStrings("addresses", args.Addresses),
		zap.Stringer("encoding", args.Encoding),
	)

	if len(args.Addresses) == 0 {
		return errNoAddresses
	}
	if len(args.Addresses) > maxGetUTXOsAddrs {
		return fmt.Errorf("number of addresses given, %d, exceeds maximum, %d", len(args.Addresses), maxGetUTXOsAddrs)
	}

	if args.SourceChain == "" {
		return errNoSourceChain
	}

	sourceChainID, err := service.Context.BCLookup.Lookup(args.SourceChain)
	if err != nil {
		return fmt.Errorf("problem parsing source chainID %q: %w", args.SourceChain, err)
	}

	var addrs set.Set[ids.ShortID]
	for _, addrStr := range args.Addresses {
		addr, err := ParseServiceAddress(service.Context, addrStr)
		if err != nil {
			return fmt.Errorf("couldn't parse address %q: %w", addrStr, err)
		}
		addrs.Add(addr)
	}

	startAddr := ids.ShortEmpty
	startUTXO := ids.Empty
	if args.StartIndex.Address != "" || args.StartIndex.UTXO != "" {
		startAddr, err = ParseServiceAddress(service.Context, args.StartIndex.Address)
		if err != nil {
			return fmt.Errorf("couldn't parse start index address %q: %w", args.StartIndex.Address, err)
		}
		startUTXO, err = ids.FromString(args.StartIndex.UTXO)
		if err != nil {
			return fmt.Errorf("couldn't parse start index utxo: %w", err)
		}
	}

	service.Context.Lock.Lock()
	defer service.Context.Lock.Unlock()

	limit := int(args.Limit)

	if limit <= 0 || limit > maxUTXOsToFetch {
		limit = maxUTXOsToFetch
	}

	utxos, endAddr, endUTXOID, err := avax.GetAtomicUTXOs(
		service.Context.SharedMemory,
		atomic.Codec,
		sourceChainID,
		addrs,
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

	endAddress, err := FormatLocalAddress(service.Context, endAddr)
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}

	reply.EndIndex.Address = endAddress
	reply.EndIndex.UTXO = endUTXOID.String()
	reply.NumFetched = json.Uint64(len(utxos))
	reply.Encoding = args.Encoding
	return nil
}

func (service *AvaxAPI) IssueTx(_ *http.Request, args *api.FormattedTx, response *api.JSONTxID) error {
	service.Context.Log.Debug("API called",
		zap.String("service", "avax"),
		zap.String("method", "issueTx"),
		logging.UserString("tx", args.Tx),
		zap.Stringer("encoding", args.Encoding),
	)

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

	service.Context.Lock.Lock()
	defer service.Context.Lock.Unlock()

	err = service.Mempool.AddLocalTx(tx)
	if err != nil && !errors.Is(err, txpool.ErrAlreadyKnown) {
		return err
	}

	// If the tx was either already in the mempool or was added to the mempool,
	// we push it to the network for inclusion. If the tx was previously added
	// to the mempool through p2p gossip, this will ensure this node also pushes
	// it to the network.
	service.PushGossiper.Add(tx)
	return nil
}

// GetAtomicTxStatus returns the status of the specified transaction
func (service *AvaxAPI) GetAtomicTxStatus(_ *http.Request, args *api.JSONTxID, reply *client.GetAtomicTxStatusReply) error {
	service.Context.Log.Debug("API called",
		zap.String("service", "avax"),
		zap.String("method", "getAtomicTxStatus"),
		zap.Stringer("txID", args.TxID),
	)

	if args.TxID == ids.Empty {
		return errNilTxID
	}

	service.Context.Lock.Lock()
	defer service.Context.Lock.Unlock()

	var err error
	_, reply.Status, reply.BlockHeight, err = service.getAtomicTx(args.TxID)
	return err
}

type FormattedTx struct {
	api.FormattedTx
	BlockHeight *json.Uint64 `json:"blockHeight,omitempty"`
}

// GetAtomicTx returns the specified transaction
func (service *AvaxAPI) GetAtomicTx(_ *http.Request, args *api.GetTxArgs, reply *FormattedTx) error {
	service.Context.Log.Debug("API called",
		zap.String("service", "avax"),
		zap.String("method", "getAtomicTx"),
		zap.Stringer("txID", args.TxID),
		zap.Stringer("encoding", args.Encoding),
	)

	if args.TxID == ids.Empty {
		return errNilTxID
	}

	service.Context.Lock.Lock()
	defer service.Context.Lock.Unlock()

	tx, status, height, err := service.getAtomicTx(args.TxID)
	if err != nil {
		return err
	}
	if status == atomic.Unknown {
		return fmt.Errorf("could not find tx %s", args.TxID)
	}

	reply.Tx, err = formatting.Encode(args.Encoding, tx.SignedBytes())
	reply.Encoding = args.Encoding
	reply.BlockHeight = height
	return err
}

// getAtomicTx returns the requested transaction, status, and height.
// If the status is [atomic.Unknown], then the returned transaction will be nil.
func (service *AvaxAPI) getAtomicTx(txID ids.ID) (*atomic.Tx, atomic.Status, *json.Uint64, error) {
	tx, height, err := service.AcceptedTxs.GetByTxID(txID)
	if err == nil {
		// Since chain state updates run asynchronously with VM block
		// acceptance, avoid returning [atomic.Accepted] until the chain state
		// reaches the block containing the atomic tx.
		if service.bc != nil && height > service.bc.LastAcceptedBlock().NumberU64() {
			return tx, atomic.Processing, nil, nil
		}
		return tx, atomic.Accepted, (*json.Uint64)(&height), nil
	}
	if err != database.ErrNotFound {
		return nil, atomic.Unknown, nil, err
	}

	tx, dropped, found := service.Mempool.GetTx(txID)
	switch {
	case found && dropped:
		return tx, atomic.Dropped, nil, nil
	case found:
		return tx, atomic.Processing, nil, nil
	default:
		return nil, atomic.Unknown, nil, nil
	}
}
