// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ava-labs/libevm/common"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/txpool"
)

// service is the server-side handler for the avax RPC API.
//
// The type is unexported but its methods are exported because gorilla RPC
// reflects on them to dispatch requests.
type service struct {
	ctx          *snow.Context
	txpool       *txpool.Txpool
	pushGossiper *gossip.PushGossiper[*gossipTx]
	state        *state.State

	chainAlias  string
	hrp         string
	zeroAddress string
}

func newService(
	ctx *snow.Context,
	pool *txpool.Txpool,
	pushGossiper *gossip.PushGossiper[*gossipTx],
	db *state.State,
) (*service, error) {
	chainAlias, err := ctx.BCLookup.PrimaryAlias(ctx.ChainID)
	if err != nil {
		return nil, err
	}

	hrp := constants.GetHRP(ctx.NetworkID)
	zeroAddress, err := address.Format(chainAlias, hrp, ids.ShortEmpty[:])
	if err != nil {
		return nil, fmt.Errorf("formatting zero address: %w", err)
	}

	return &service{
		ctx:          ctx,
		txpool:       pool,
		pushGossiper: pushGossiper,
		state:        db,

		chainAlias:  chainAlias,
		hrp:         hrp,
		zeroAddress: zeroAddress,
	}, nil
}

const maxGetUTXOsLimit = 1024

// terminal IDs are used as a sentinel to indicate the end of pagination.
var (
	termAddr   = ids.ShortID(common.HexToAddress("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"))
	termUTXOID = ids.ID(common.HexToHash("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"))
)

func (s *service) GetUTXOs(_ *http.Request, a *api.GetUTXOsArgs, r *api.GetUTXOsReply) error {
	s.ctx.Log.Debug("API called",
		zap.String("service", "avax"),
		zap.String("method", "getUTXOs"),
		logging.UserStrings("addresses", a.Addresses),
		zap.Stringer("encoding", a.Encoding),
	)

	sourceChainID, err := s.ctx.BCLookup.Lookup(a.SourceChain)
	if err != nil {
		return fmt.Errorf("parsing source chainID %q: %w", a.SourceChain, err)
	}

	const maxAddrs = 1024
	if len(a.Addresses) > maxAddrs {
		return fmt.Errorf("too many addresses: %d exceeds %d", len(a.Addresses), maxAddrs)
	}

	addrs := make([][]byte, len(a.Addresses))
	for i, str := range a.Addresses {
		addr, err := s.parseAddress(str)
		if err != nil {
			return fmt.Errorf("parsing address %q: %w", str, err)
		}
		addrs[i] = addr[:]
	}

	// Set the response encoding here in case the client provided the terminal
	// cursor.
	r.Encoding = a.Encoding

	var (
		startAddr ids.ShortID
		startUTXO ids.ID
	)
	if a.StartIndex != (api.Index{}) {
		startAddr, err = s.parseAddress(a.StartIndex.Address)
		if err != nil {
			return fmt.Errorf("parsing start address %q: %w", a.StartIndex.Address, err)
		}
		startUTXO, err = ids.FromString(a.StartIndex.UTXO)
		if err != nil {
			return fmt.Errorf("parsing start utxoID %q: %w", a.StartIndex.UTXO, err)
		}
		if startAddr == termAddr && startUTXO == termUTXOID {
			// Client provided the terminal index, so there are no more results.
			r.UTXOs = []string{}
			r.EndIndex.Address = s.zeroAddress
			r.EndIndex.UTXO = ids.Empty.String()
			return nil
		}
	}

	limit := a.Limit
	if limit == 0 || limit > maxGetUTXOsLimit {
		limit = maxGetUTXOsLimit
	}

	// [atomic.SharedMemory.Indexed] iterates inclusively from startAddr and
	// startUTXO, so we fetch one extra UTXO to return the next index.
	utxos, nextAddr, nextUTXO, err := s.ctx.SharedMemory.Indexed(
		sourceChainID,
		addrs,
		startAddr[:],
		startUTXO[:],
		int(limit)+1,
	)
	if err != nil {
		return fmt.Errorf("retrieving UTXOs: %w", err)
	}

	var (
		endAddr ids.ShortID
		endUTXO ids.ID
	)
	if len(utxos) > int(limit) {
		utxos = utxos[:limit]
		endAddr, _ = ids.ToShortID(nextAddr)
		endUTXO, _ = ids.ToID(nextUTXO)
	} else {
		endAddr = termAddr
		endUTXO = termUTXOID
	}

	r.UTXOs = make([]string, len(utxos))
	for i, utxo := range utxos {
		r.UTXOs[i], err = formatting.Encode(a.Encoding, utxo)
		if err != nil {
			return fmt.Errorf("encoding utxo: %w", err)
		}
	}

	r.EndIndex.Address, err = address.Format(s.chainAlias, s.hrp, endAddr[:])
	if err != nil {
		return fmt.Errorf("formatting address: %w", err)
	}
	r.EndIndex.UTXO = endUTXO.String()
	r.NumFetched = json.Uint64(len(utxos))
	return nil
}

// parseAddress parses str as either a human-readable address or cb58-encoded
// address.
func (s *service) parseAddress(str string) (ids.ShortID, error) {
	if a, err := ids.ShortFromString(str); err == nil {
		return a, nil
	}

	chainAlias, hrp, addrBytes, err := address.Parse(str)
	if err != nil {
		return ids.ShortID{}, err
	}
	if hrp != s.hrp {
		return ids.ShortID{}, fmt.Errorf("expected hrp %q but got %q", s.hrp, hrp)
	}
	chainID, err := s.ctx.BCLookup.Lookup(chainAlias)
	if err != nil {
		return ids.ShortID{}, err
	}
	if chainID != s.ctx.ChainID {
		return ids.ShortID{}, fmt.Errorf("expected chainID %q but got %q", s.ctx.ChainID, chainID)
	}
	return ids.ToShortID(addrBytes)
}

var errIssuingTx = errors.New("issuing tx")

func (s *service) IssueTx(_ *http.Request, a *api.FormattedTx, r *api.JSONTxID) error {
	s.ctx.Log.Debug("API called",
		zap.String("service", "avax"),
		zap.String("method", "issueTx"),
		logging.UserString("tx", a.Tx),
		zap.Stringer("encoding", a.Encoding),
	)

	txBytes, err := formatting.Decode(a.Encoding, a.Tx)
	if err != nil {
		return fmt.Errorf("decoding transaction: %w", err)
	}
	t, err := tx.Parse(txBytes)
	if err != nil {
		return fmt.Errorf("parsing transaction: %w", err)
	}

	if err := s.txpool.Add(t); err != nil && !errors.Is(err, txpool.ErrAlreadyKnown) {
		return fmt.Errorf("%w: %w", errIssuingTx, err)
	}

	// Even if already in the pool from a peer's gossip, push it to peers.
	s.pushGossiper.Add(toGossipTx(t))

	r.TxID = t.ID()
	return nil
}

// GetTxReply is the response returned by [service.GetAtomicTx].
//
// It MUST be exported for gorilla RPC to publicly expose [service.GetAtomicTx].
type GetTxReply struct {
	api.FormattedTx
	Height json.Uint64 `json:"blockHeight"`
}

var errFetchingTx = errors.New("fetching tx")

func (s *service) GetAtomicTx(_ *http.Request, a *api.GetTxArgs, r *GetTxReply) error {
	s.ctx.Log.Debug("API called",
		zap.String("service", "avax"),
		zap.String("method", "getAtomicTx"),
		zap.Stringer("txID", a.TxID),
		zap.Stringer("encoding", a.Encoding),
	)

	t, height, err := s.state.GetTx(a.TxID)
	if err != nil {
		return fmt.Errorf("%w: %w", errFetchingTx, err)
	}
	txBytes, err := t.Bytes()
	if err != nil {
		return fmt.Errorf("marshalling tx: %w", err)
	}
	r.Tx, err = formatting.Encode(a.Encoding, txBytes)
	if err != nil {
		return fmt.Errorf("encoding tx: %w", err)
	}
	r.Encoding = a.Encoding
	r.Height = json.Uint64(height)
	return nil
}

// Client interacts with the avax API served by the C-Chain.
type Client struct {
	r rpc.EndpointRequester
}

const (
	cchainHTTPPrefix = "/ext/" + constants.ChainAliasPrefix + "/C"
	avaxHTTPPath     = cchainHTTPPrefix + avaxHTTPExtensionPath
)

// NewClient returns a [Client] that targets the C-Chain reachable at uri.
func NewClient(uri string) *Client {
	return &Client{
		r: rpc.NewEndpointRequester(uri + avaxHTTPPath),
	}
}

// The most efficient encoding format is used for all calls by the client.
const clientEncoding = formatting.HexNC

// GetUTXOs returns the UTXOs controlled by addrs that have been exported to
// the C-Chain from sourceChain.
//
// Responses are paginated via startAddr and startUTXOID. To fetch all UTXOs,
// the zero values can be passed on the first call and the returned
// (endAddr, endUTXOID) on each subsequent call until fewer than limit
// results are returned.
func (c *Client) GetUTXOs(
	ctx context.Context,
	addrs []ids.ShortID,
	sourceChain ids.ID,
	limit uint32,
	startAddr ids.ShortID,
	startUTXOID ids.ID,
	options ...rpc.Option,
) ([]*avax.UTXO, ids.ShortID, ids.ID, error) {
	res := &api.GetUTXOsReply{}
	err := c.r.SendRequest(ctx, "avax.getUTXOs", &api.GetUTXOsArgs{
		Addresses:   ids.ShortIDsToStrings(addrs),
		SourceChain: sourceChain.String(),
		Limit:       json.Uint32(limit),
		StartIndex: api.Index{
			Address: startAddr.String(),
			UTXO:    startUTXOID.String(),
		},
		Encoding: clientEncoding,
	}, res, options...)
	if err != nil {
		return nil, ids.ShortID{}, ids.Empty, fmt.Errorf("sending request: %w", err)
	}

	utxos := make([]*avax.UTXO, len(res.UTXOs))
	for i, raw := range res.UTXOs {
		utxoBytes, err := formatting.Decode(res.Encoding, raw)
		if err != nil {
			return nil, ids.ShortID{}, ids.Empty, fmt.Errorf("decoding utxo %d: %w", i, err)
		}
		utxos[i], err = tx.ParseUTXO(utxoBytes)
		if err != nil {
			return nil, ids.ShortID{}, ids.Empty, fmt.Errorf("parsing utxo %d: %w", i, err)
		}
	}
	endAddr, err := address.ParseToID(res.EndIndex.Address)
	if err != nil {
		return nil, ids.ShortID{}, ids.Empty, fmt.Errorf("parsing end address: %w", err)
	}
	endUTXOID, err := ids.FromString(res.EndIndex.UTXO)
	if err != nil {
		return nil, ids.ShortID{}, ids.Empty, fmt.Errorf("parsing end utxoID: %w", err)
	}
	return utxos, endAddr, endUTXOID, nil
}

// IssueTx submits t to the txpool.
func (c *Client) IssueTx(ctx context.Context, t *tx.Tx, options ...rpc.Option) error {
	txBytes, err := t.Bytes()
	if err != nil {
		return fmt.Errorf("marshalling tx: %w", err)
	}
	txStr, err := formatting.Encode(clientEncoding, txBytes)
	if err != nil {
		return fmt.Errorf("encoding tx: %w", err)
	}

	err = c.r.SendRequest(ctx, "avax.issueTx", &api.FormattedTx{
		Tx:       txStr,
		Encoding: clientEncoding,
	}, &api.JSONTxID{}, options...)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	return nil
}

// GetTx returns an accepted cross-chain transaction along with the block height
// at which it was accepted.
func (c *Client) GetTx(ctx context.Context, txID ids.ID, options ...rpc.Option) (*tx.Tx, uint64, error) {
	res := &GetTxReply{}
	err := c.r.SendRequest(ctx, "avax.getAtomicTx", &api.GetTxArgs{
		TxID:     txID,
		Encoding: clientEncoding,
	}, res, options...)
	if err != nil {
		return nil, 0, fmt.Errorf("sending request: %w", err)
	}

	txBytes, err := formatting.Decode(res.Encoding, res.Tx)
	if err != nil {
		return nil, 0, fmt.Errorf("decoding tx: %w", err)
	}
	t, err := tx.Parse(txBytes)
	if err != nil {
		return nil, 0, fmt.Errorf("parsing tx: %w", err)
	}
	return t, uint64(res.Height), nil
}
