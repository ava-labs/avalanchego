// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"errors"
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/client"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/txpool"

	atomicstate "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"
	avalanchegossip "github.com/ava-labs/avalanchego/network/p2p/gossip"
)

type customAPI struct {
	ctx *snow.Context

	mempool      *txpool.Mempool
	pushGossiper *avalanchegossip.PushGossiper[*atomic.Tx]
	acceptedTxs  *atomicstate.AtomicRepository
}

func (c *customAPI) GetUTXOs(_ *http.Request, a *api.GetUTXOsArgs, r *api.GetUTXOsReply) error {
	c.ctx.Log.Debug("API called",
		zap.String("service", "avax"),
		zap.String("method", "getUTXOs"),
		logging.UserStrings("addresses", a.Addresses),
		zap.Stringer("encoding", a.Encoding),
	)

	const maxAddrs = 1024
	if len(a.Addresses) > maxAddrs {
		return fmt.Errorf("number of addresses given, %d, exceeds maximum, %d", len(a.Addresses), maxAddrs)
	}

	sourceChainID, err := c.ctx.BCLookup.Lookup(a.SourceChain)
	if err != nil {
		return fmt.Errorf("problem parsing source chainID %q: %w", a.SourceChain, err)
	}

	var addrs set.Set[ids.ShortID]
	for _, s := range a.Addresses {
		addr, err := c.parseAddress(s)
		if err != nil {
			return fmt.Errorf("couldn't parse address %q: %w", s, err)
		}
		addrs.Add(addr)
	}

	var (
		startAddr ids.ShortID
		startUTXO ids.ID
	)
	if a.StartIndex != (api.Index{}) {
		startAddr, err = c.parseAddress(a.StartIndex.Address)
		if err != nil {
			return fmt.Errorf("couldn't parse start index address %q: %w", a.StartIndex.Address, err)
		}
		startUTXO, err = ids.FromString(a.StartIndex.UTXO)
		if err != nil {
			return fmt.Errorf("couldn't parse start index utxo: %w", err)
		}
	}

	const maxLimit = 1024
	limit := int(min(a.Limit, maxLimit))
	utxos, endAddr, endUTXOID, err := avax.GetAtomicUTXOs(
		c.ctx.SharedMemory,
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

	r.UTXOs = make([]string, len(utxos))
	for i, utxo := range utxos {
		b, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
		if err != nil {
			return fmt.Errorf("problem marshalling UTXO: %w", err)
		}
		r.UTXOs[i], err = formatting.Encode(a.Encoding, b)
		if err != nil {
			return fmt.Errorf("problem encoding utxo: %w", err)
		}
	}

	r.EndIndex.Address, err = c.formatAddress(endAddr)
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}

	r.EndIndex.UTXO = endUTXOID.String()
	r.NumFetched = json.Uint64(len(utxos))
	r.Encoding = a.Encoding
	return nil
}

func (c *customAPI) parseAddress(s string) (ids.ShortID, error) {
	a, err := ids.ShortFromString(s)
	if err == nil {
		return a, nil
	}

	chainAlias, hrp, bytes, err := address.Parse(s)
	if err != nil {
		return ids.ShortID{}, err
	}
	if want := constants.GetHRP(c.ctx.NetworkID); hrp != want {
		return ids.ShortID{}, fmt.Errorf("expected hrp %q but got %q", want, hrp)
	}

	chainID, err := c.ctx.BCLookup.Lookup(chainAlias)
	if err != nil {
		return ids.ShortID{}, err
	}
	if chainID != c.ctx.ChainID {
		return ids.ShortID{}, fmt.Errorf("expected chainID to be %q but was %q", c.ctx.ChainID, chainID)
	}

	return ids.ToShortID(bytes)
}

func (c *customAPI) formatAddress(addr ids.ShortID) (string, error) {
	chainAlias, err := c.ctx.BCLookup.PrimaryAlias(c.ctx.ChainID)
	if err != nil {
		return "", err
	}
	hrp := constants.GetHRP(c.ctx.NetworkID)
	return address.Format(chainAlias, hrp, addr.Bytes())
}

func (c *customAPI) IssueTx(_ *http.Request, a *api.FormattedTx, r *api.JSONTxID) error {
	c.ctx.Log.Debug("API called",
		zap.String("service", "avax"),
		zap.String("method", "issueTx"),
		logging.UserString("tx", a.Tx),
		zap.Stringer("encoding", a.Encoding),
	)

	txBytes, err := formatting.Decode(a.Encoding, a.Tx)
	if err != nil {
		return fmt.Errorf("problem decoding transaction: %w", err)
	}
	tx, err := parseTx(txBytes)
	if err != nil {
		return fmt.Errorf("problem parsing transaction: %w", err)
	}

	r.TxID = tx.ID()
	err = c.mempool.Add(tx)
	if err == nil || errors.Is(err, txpool.ErrAlreadyKnown) {
		// If the tx was added to the mempool, or was previously included, we
		// push it to the network for inclusion. This ensures this node will
		// push the tx, even if it was previously seen by p2p gossip.
		c.pushGossiper.Add(tx)
	}
	return err
}

func parseTx(txBytes []byte) (*atomic.Tx, error) {
	tx := &atomic.Tx{}
	if _, err := atomic.Codec.Unmarshal(txBytes, tx); err != nil {
		return nil, fmt.Errorf("%T.Unmarshal(): %v", atomic.Codec, err)
	}
	if err := tx.Sign(atomic.Codec, nil); err != nil {
		return nil, fmt.Errorf("%T.Sign(): %v", atomic.Codec, err)
	}
	return tx, nil
}

// GetAtomicTxStatusReply defines the GetAtomicTxStatus replies returned from the API
type GetAtomicTxStatusReply struct {
	Status      atomic.Status `json:"status"`
	BlockHeight *json.Uint64  `json:"blockHeight,omitempty"`
}

// GetAtomicTxStatus returns the status of the specified transaction
func (service *customAPI) GetAtomicTxStatus(_ *http.Request, args *api.JSONTxID, reply *client.GetAtomicTxStatusReply) error {
	service.Context.Log.Debug("API called",
		zap.String("service", "avax"),
		zap.String("method", "getAtomicTxStatus"),
		zap.Stringer("txID", args.TxID),
	)

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
func (c *customAPI) GetAtomicTx(_ *http.Request, args *api.GetTxArgs, reply *FormattedTx) error {
	c.ctx.Log.Debug("API called",
		zap.String("service", "avax"),
		zap.String("method", "getAtomicTx"),
		zap.Stringer("txID", args.TxID),
		zap.Stringer("encoding", args.Encoding),
	)

	c.ctx.Lock.Lock()
	defer c.ctx.Lock.Unlock()

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

func (c *customAPI) getTx(txID ids.ID) (*atomic.Tx, Status, *json.Uint64, error) {
	tx, height, err := c.acceptedTxs.GetByTxID(txID)
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
