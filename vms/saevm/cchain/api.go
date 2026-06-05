// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/holiman/uint256"
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
	gossipSet    *gossip.BloomSet[*gossipTx]
	pushGossiper *gossip.PushGossiper[*gossipTx]
	state        *state.State

	chainAlias  string
	hrp         string
	zeroAddress string
}

func newService(
	ctx *snow.Context,
	gossipSet *gossip.BloomSet[*gossipTx],
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
		gossipSet:    gossipSet,
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
	termAddr   ids.ShortID = new(uint256.Int).SetAllOne().Bytes20()
	termUTXOID ids.ID      = new(uint256.Int).SetAllOne().Bytes32()
)

func (s *service) GetUTXOs(_ *http.Request, args *api.GetUTXOsArgs, resp *api.GetUTXOsReply) error {
	s.ctx.Log.Debug("API called",
		zap.String("service", "avax"),
		zap.String("method", "getUTXOs"),
		logging.UserStrings("addresses", args.Addresses),
		zap.Stringer("encoding", args.Encoding),
	)

	sourceChainID, err := s.ctx.BCLookup.Lookup(args.SourceChain)
	if err != nil {
		return fmt.Errorf("parsing source chainID %q: %w", args.SourceChain, err)
	}

	const maxAddrs = 1024
	if len(args.Addresses) > maxAddrs {
		return fmt.Errorf("too many addresses: %d exceeds %d", len(args.Addresses), maxAddrs)
	}

	addrs := make([][]byte, len(args.Addresses))
	for i, str := range args.Addresses {
		addr, err := s.parseAddress(str)
		if err != nil {
			return fmt.Errorf("parsing address %q: %w", str, err)
		}
		addrs[i] = addr[:]
	}

	// Set the response encoding here in case the client provided the terminal
	// cursor, which would return early below.
	resp.Encoding = args.Encoding

	var (
		startAddr ids.ShortID
		startUTXO ids.ID
	)
	if args.StartIndex != (api.Index{}) {
		startAddr, err = s.parseAddress(args.StartIndex.Address)
		if err != nil {
			return fmt.Errorf("parsing start address %q: %w", args.StartIndex.Address, err)
		}
		startUTXO, err = ids.FromString(args.StartIndex.UTXO)
		if err != nil {
			return fmt.Errorf("parsing start utxoID %q: %w", args.StartIndex.UTXO, err)
		}
		if startAddr == termAddr && startUTXO == termUTXOID {
			// Client provided the terminal index, so there are no more results.
			resp.UTXOs = []string{}
			resp.EndIndex.Address = s.zeroAddress
			resp.EndIndex.UTXO = ids.Empty.String()
			return nil
		}
	}

	limit := args.Limit
	if limit == 0 || limit > maxGetUTXOsLimit {
		limit = maxGetUTXOsLimit
	}

	// [atomic.SharedMemory.Indexed] returns the last address and last UTXO, so
	// we fetch one extra UTXO to determine the next index.
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

	// Now we convert the closed interval [start, end] from shared memory to the
	// half-open interval [start, end) expected for the API.
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

	resp.UTXOs = make([]string, len(utxos))
	for i, utxo := range utxos {
		resp.UTXOs[i], err = formatting.Encode(args.Encoding, utxo)
		if err != nil {
			return fmt.Errorf("encoding utxo: %w", err)
		}
	}

	resp.EndIndex.Address, err = address.Format(s.chainAlias, s.hrp, endAddr[:])
	if err != nil {
		return fmt.Errorf("formatting address: %w", err)
	}
	resp.EndIndex.UTXO = endUTXO.String()
	resp.NumFetched = json.Uint64(len(utxos))
	return nil
}

// parseAddress parses str as either a human-readable address or cb58-encoded
// address.
func (s *service) parseAddress(str string) (ids.ShortID, error) {
	if a, err := ids.ShortFromString(str); err == nil { // if NO error
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

func (s *service) IssueTx(_ *http.Request, args *api.FormattedTx, resp *api.JSONTxID) error {
	s.ctx.Log.Debug("API called",
		zap.String("service", "avax"),
		zap.String("method", "issueTx"),
		logging.UserString("tx", args.Tx),
		zap.Stringer("encoding", args.Encoding),
	)

	t, err := decodeTx(args.Tx, args.Encoding)
	if err != nil {
		return err
	}
	gossipTx := toGossipTx(t)
	if err := s.gossipSet.Add(gossipTx); err != nil && !errors.Is(err, txpool.ErrAlreadyKnown) {
		return fmt.Errorf("%w: %w", errIssuingTx, err)
	}

	// Always push, even if already in the pool. Otherwise a malicious RPC could
	// suppress a tx from being efficiently gossiped to validators.
	s.pushGossiper.Add(gossipTx)

	resp.TxID = t.ID()
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

func (s *service) GetAtomicTx(_ *http.Request, args *api.GetTxArgs, resp *GetTxReply) error {
	s.ctx.Log.Debug("API called",
		zap.String("service", "avax"),
		zap.String("method", "getAtomicTx"),
		zap.Stringer("txID", args.TxID),
		zap.Stringer("encoding", args.Encoding),
	)

	t, height, err := s.state.GetTx(args.TxID)
	if err != nil {
		return fmt.Errorf("%w: %w", errFetchingTx, err)
	}
	resp.Tx, err = encodeTx(t, args.Encoding)
	if err != nil {
		return fmt.Errorf("encoding tx: %w", err)
	}
	resp.Encoding = args.Encoding
	resp.Height = json.Uint64(height)
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
) (_ []*avax.UTXO, endAddr ids.ShortID, endUTXOID ids.ID, _ error) {
	var resp api.GetUTXOsReply
	err := c.r.SendRequest(
		ctx,
		"avax.getUTXOs",
		&api.GetUTXOsArgs{
			Addresses:   ids.ShortIDsToStrings(addrs),
			SourceChain: sourceChain.String(),
			Limit:       json.Uint32(limit),
			StartIndex: api.Index{
				Address: startAddr.String(),
				UTXO:    startUTXOID.String(),
			},
			Encoding: clientEncoding,
		},
		&resp,
		options...,
	)
	if err != nil {
		return nil, ids.ShortID{}, ids.Empty, fmt.Errorf("sending request: %w", err)
	}

	utxos := make([]*avax.UTXO, len(resp.UTXOs))
	for i, raw := range resp.UTXOs {
		utxoBytes, err := formatting.Decode(resp.Encoding, raw)
		if err != nil {
			return nil, ids.ShortID{}, ids.Empty, fmt.Errorf("decoding utxo %d: %w", i, err)
		}
		utxos[i], err = tx.ParseUTXO(utxoBytes)
		if err != nil {
			return nil, ids.ShortID{}, ids.Empty, fmt.Errorf("parsing utxo %d: %w", i, err)
		}
	}
	endAddr, err = address.ParseToID(resp.EndIndex.Address)
	if err != nil {
		return nil, ids.ShortID{}, ids.Empty, fmt.Errorf("parsing end address: %w", err)
	}
	endUTXOID, err = ids.FromString(resp.EndIndex.UTXO)
	if err != nil {
		return nil, ids.ShortID{}, ids.Empty, fmt.Errorf("parsing end utxoID: %w", err)
	}
	return utxos, endAddr, endUTXOID, nil
}

// IssueTx submits t to the txpool.
func (c *Client) IssueTx(ctx context.Context, t *tx.Tx, options ...rpc.Option) error {
	txStr, err := encodeTx(t, clientEncoding)
	if err != nil {
		return err
	}

	err = c.r.SendRequest(
		ctx,
		"avax.issueTx",
		&api.FormattedTx{
			Tx:       txStr,
			Encoding: clientEncoding,
		},
		&api.JSONTxID{},
		options...,
	)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	return nil
}

// GetTx returns an accepted cross-chain transaction along with the block height
// at which it was accepted.
func (c *Client) GetTx(ctx context.Context, txID ids.ID, options ...rpc.Option) (*tx.Tx, uint64, error) {
	var resp GetTxReply
	err := c.r.SendRequest(
		ctx,
		"avax.getAtomicTx",
		&api.GetTxArgs{
			TxID:     txID,
			Encoding: clientEncoding,
		},
		&resp,
		options...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("sending request: %w", err)
	}

	t, err := decodeTx(resp.Tx, resp.Encoding)
	if err != nil {
		return nil, 0, err
	}
	return t, uint64(resp.Height), nil
}

func encodeTx(t *tx.Tx, encoding formatting.Encoding) (string, error) {
	b, err := t.Bytes()
	if err != nil {
		return "", fmt.Errorf("marshalling tx: %w", err)
	}
	s, err := formatting.Encode(encoding, b)
	if err != nil {
		return "", fmt.Errorf("encoding tx: %w", err)
	}
	return s, nil
}

func decodeTx(s string, encoding formatting.Encoding) (*tx.Tx, error) {
	b, err := formatting.Decode(encoding, s)
	if err != nil {
		return nil, fmt.Errorf("decoding tx: %w", err)
	}
	t, err := tx.Parse(b)
	if err != nil {
		return nil, fmt.Errorf("parsing tx: %w", err)
	}
	return t, nil
}
