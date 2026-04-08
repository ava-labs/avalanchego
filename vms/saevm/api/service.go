// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/ava-labs/strevm/sae/rpc"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/state"
	"github.com/ava-labs/avalanchego/vms/saevm/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/txpool"
)

type Service struct {
	ctx          *snow.Context
	backend      rpc.GethBackends
	mempool      *txpool.Mempool
	pushGossiper *gossip.PushGossiper[*tx.Tx]
	db           database.KeyValueReader
}

func NewService(
	ctx *snow.Context,
	backend rpc.GethBackends,
	mempool *txpool.Mempool,
	pushGossiper *gossip.PushGossiper[*tx.Tx],
	db database.KeyValueReader,
) *Service {
	return &Service{
		ctx,
		backend,
		mempool,
		pushGossiper,
		db,
	}
}

func (s *Service) GetUTXOs(_ *http.Request, a *api.GetUTXOsArgs, r *api.GetUTXOsReply) error {
	s.ctx.Log.Debug("API called",
		zap.String("service", "avax"),
		zap.String("method", "getUTXOs"),
		logging.UserStrings("addresses", a.Addresses),
		zap.Stringer("encoding", a.Encoding),
	)

	const maxAddrs = 1024
	if len(a.Addresses) > maxAddrs {
		return fmt.Errorf("number of addresses given, %d, exceeds maximum, %d", len(a.Addresses), maxAddrs)
	}

	sourceChainID, err := s.ctx.BCLookup.Lookup(a.SourceChain)
	if err != nil {
		return fmt.Errorf("problem parsing source chainID %q: %w", a.SourceChain, err)
	}

	var addrs set.Set[ids.ShortID]
	for _, str := range a.Addresses {
		addr, err := s.parseAddress(str)
		if err != nil {
			return fmt.Errorf("couldn't parse address %q: %w", str, err)
		}
		addrs.Add(addr)
	}

	var (
		startAddr ids.ShortID
		startUTXO ids.ID
	)
	if a.StartIndex != (api.Index{}) {
		startAddr, err = s.parseAddress(a.StartIndex.Address)
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
		s.ctx.SharedMemory,
		tx.Codec,
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
		b, err := tx.Codec.Marshal(tx.CodecVersion, utxo)
		if err != nil {
			return fmt.Errorf("problem marshalling UTXO: %w", err)
		}
		r.UTXOs[i], err = formatting.Encode(a.Encoding, b)
		if err != nil {
			return fmt.Errorf("problem encoding utxo: %w", err)
		}
	}

	r.EndIndex.Address, err = s.formatAddress(endAddr)
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}

	r.EndIndex.UTXO = endUTXOID.String()
	r.NumFetched = json.Uint64(len(utxos))
	r.Encoding = a.Encoding
	return nil
}

func (s *Service) parseAddress(str string) (ids.ShortID, error) {
	if a, err := ids.ShortFromString(str); err == nil {
		return a, nil
	}

	chainAlias, hrp, bytes, err := address.Parse(str)
	if err != nil {
		return ids.ShortID{}, err
	}
	if want := constants.GetHRP(s.ctx.NetworkID); hrp != want {
		return ids.ShortID{}, fmt.Errorf("expected hrp %q but got %q", want, hrp)
	}

	chainID, err := s.ctx.BCLookup.Lookup(chainAlias)
	if err != nil {
		return ids.ShortID{}, err
	}
	if chainID != s.ctx.ChainID {
		return ids.ShortID{}, fmt.Errorf("expected chainID to be %q but was %q", s.ctx.ChainID, chainID)
	}

	return ids.ToShortID(bytes)
}

func (s *Service) formatAddress(addr ids.ShortID) (string, error) {
	chainAlias, err := s.ctx.BCLookup.PrimaryAlias(s.ctx.ChainID)
	if err != nil {
		return "", err
	}
	hrp := constants.GetHRP(s.ctx.NetworkID)
	return address.Format(chainAlias, hrp, addr.Bytes())
}

func (s *Service) IssueTx(_ *http.Request, a *api.FormattedTx, r *api.JSONTxID) error {
	s.ctx.Log.Debug("API called",
		zap.String("service", "avax"),
		zap.String("method", "issueTx"),
		logging.UserString("tx", a.Tx),
		zap.Stringer("encoding", a.Encoding),
	)

	txBytes, err := formatting.Decode(a.Encoding, a.Tx)
	if err != nil {
		return fmt.Errorf("problem decoding transaction: %w", err)
	}
	tx, err := tx.Parse(txBytes)
	if err != nil {
		return fmt.Errorf("problem parsing transaction: %w", err)
	}

	txID, err := tx.ID()
	if err != nil {
		return fmt.Errorf("problem getting transaction ID: %w", err)
	}

	err = s.mempool.Add(tx)
	if err == nil || errors.Is(err, txpool.ErrAlreadyKnown) {
		// If the tx was added to the mempool, or was previously included, we
		// push it to the network for inclusion. This ensures this node will
		// push the tx, even if it was previously seen by p2p gossip.
		s.pushGossiper.Add(tx)
	}

	r.TxID = txID
	return err
}

type TxStatus struct {
	Status Status       `json:"status"`
	Height *json.Uint64 `json:"blockHeight,omitempty"`
}

func (s *Service) GetAtomicTxStatus(_ *http.Request, a *api.JSONTxID, r *TxStatus) error {
	s.ctx.Log.Debug("API called",
		zap.String("service", "avax"),
		zap.String("method", "getAtomicTxStatus"),
		zap.Stringer("txID", a.TxID),
	)

	_, height, err := s.readTx(a.TxID)
	if errors.Is(err, database.ErrNotFound) {
		r.Status = Unknown
		return nil
	}
	if err != nil {
		return err
	}

	r.Status = Accepted
	r.Height = (*json.Uint64)(&height)
	return nil
}

type Tx struct {
	api.FormattedTx
	Height json.Uint64 `json:"blockHeight"`
}

func (s *Service) GetAtomicTx(_ *http.Request, a *api.GetTxArgs, r *Tx) error {
	s.ctx.Log.Debug("API called",
		zap.String("service", "avax"),
		zap.String("method", "getAtomicTx"),
		zap.Stringer("txID", a.TxID),
		zap.Stringer("encoding", a.Encoding),
	)

	tx, height, err := s.readTx(a.TxID)
	if err != nil {
		return err
	}
	txBytes, err := tx.Bytes()
	if err != nil {
		return fmt.Errorf("problem getting transaction bytes: %w", err)
	}

	r.Tx, err = formatting.Encode(a.Encoding, txBytes)
	if err != nil {
		return err
	}

	r.Encoding = a.Encoding
	r.Height = json.Uint64(height)
	return nil
}

func (s *Service) readTx(txID ids.ID) (*tx.Tx, uint64, error) {
	tx, height, err := state.ReadTxByID(s.db, txID)
	if err != nil {
		return nil, 0, err
	}

	// Because the transaction is written to disk before the block is finished
	// executing, it's possible for the transaction to be found in the database,
	// but the block that includes it to not be fully executed yet. In this
	// case, we return "unknown" instead of "accepted" until the block is
	// fully executed.
	if lastExecuted := s.backend.CurrentHeader(); height > lastExecuted.Number.Uint64() {
		return nil, 0, database.ErrNotFound
	}

	return tx, height, nil
}
