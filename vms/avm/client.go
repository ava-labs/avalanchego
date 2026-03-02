// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

var ErrRejected = errors.New("rejected")

type Client struct {
	Requester rpc.EndpointRequester
}

func NewClient(uri, chain string) *Client {
	path := fmt.Sprintf(
		"%s/ext/%s/%s",
		uri,
		constants.ChainAliasPrefix,
		chain,
	)
	return &Client{
		Requester: rpc.NewEndpointRequester(path),
	}
}

// GetBlock returns the block with the given id.
func (c *Client) GetBlock(ctx context.Context, blkID ids.ID, options ...rpc.Option) ([]byte, error) {
	res := &api.FormattedBlock{}
	err := c.Requester.SendRequest(ctx, "avm.getBlock", &api.GetBlockArgs{
		BlockID:  blkID,
		Encoding: formatting.HexNC,
	}, res, options...)
	if err != nil {
		return nil, err
	}
	return formatting.Decode(res.Encoding, res.Block)
}

// GetBlockByHeight returns the block at the given height.
func (c *Client) GetBlockByHeight(ctx context.Context, height uint64, options ...rpc.Option) ([]byte, error) {
	res := &api.FormattedBlock{}
	err := c.Requester.SendRequest(ctx, "avm.getBlockByHeight", &api.GetBlockByHeightArgs{
		Height:   json.Uint64(height),
		Encoding: formatting.HexNC,
	}, res, options...)
	if err != nil {
		return nil, err
	}
	return formatting.Decode(res.Encoding, res.Block)
}

// GetHeight returns the height of the last accepted block.
func (c *Client) GetHeight(ctx context.Context, options ...rpc.Option) (uint64, error) {
	res := &api.GetHeightResponse{}
	err := c.Requester.SendRequest(ctx, "avm.getHeight", struct{}{}, res, options...)
	return uint64(res.Height), err
}

// IssueTx issues a transaction to a node and returns the TxID
func (c *Client) IssueTx(ctx context.Context, txBytes []byte, options ...rpc.Option) (ids.ID, error) {
	txStr, err := formatting.Encode(formatting.Hex, txBytes)
	if err != nil {
		return ids.Empty, err
	}
	res := &api.JSONTxID{}
	err = c.Requester.SendRequest(ctx, "avm.issueTx", &api.FormattedTx{
		Tx:       txStr,
		Encoding: formatting.Hex,
	}, res, options...)
	return res.TxID, err
}

// GetTxStatus returns the status of [txID]
//
// Deprecated: GetTxStatus only returns Accepted or Unknown, GetTx should be
// used instead to determine if the tx was accepted.
func (c *Client) GetTxStatus(ctx context.Context, txID ids.ID, options ...rpc.Option) (choices.Status, error) {
	res := &GetTxStatusReply{}
	err := c.Requester.SendRequest(ctx, "avm.getTxStatus", &api.JSONTxID{
		TxID: txID,
	}, res, options...)
	return res.Status, err
}

// GetTx returns the byte representation of txID.
func (c *Client) GetTx(ctx context.Context, txID ids.ID, options ...rpc.Option) ([]byte, error) {
	res := &api.FormattedTx{}
	err := c.Requester.SendRequest(ctx, "avm.getTx", &api.GetTxArgs{
		TxID:     txID,
		Encoding: formatting.Hex,
	}, res, options...)
	if err != nil {
		return nil, err
	}
	return formatting.Decode(res.Encoding, res.Tx)
}

// GetUTXOs returns the byte representation of the UTXOs controlled by addrs.
func (c *Client) GetUTXOs(
	ctx context.Context,
	addrs []ids.ShortID,
	limit uint32,
	startAddress ids.ShortID,
	startUTXOID ids.ID,
	options ...rpc.Option,
) ([][]byte, ids.ShortID, ids.ID, error) {
	return c.GetAtomicUTXOs(ctx, addrs, "", limit, startAddress, startUTXOID, options...)
}

// GetAtomicUTXOs returns the byte representation of the atomic UTXOs controlled
// by addrs from sourceChain.
func (c *Client) GetAtomicUTXOs(
	ctx context.Context,
	addrs []ids.ShortID,
	sourceChain string,
	limit uint32,
	startAddress ids.ShortID,
	startUTXOID ids.ID,
	options ...rpc.Option,
) ([][]byte, ids.ShortID, ids.ID, error) {
	res := &api.GetUTXOsReply{}
	err := c.Requester.SendRequest(ctx, "avm.getUTXOs", &api.GetUTXOsArgs{
		Addresses:   ids.ShortIDsToStrings(addrs),
		SourceChain: sourceChain,
		Limit:       json.Uint32(limit),
		StartIndex: api.Index{
			Address: startAddress.String(),
			UTXO:    startUTXOID.String(),
		},
		Encoding: formatting.Hex,
	}, res, options...)
	if err != nil {
		return nil, ids.ShortID{}, ids.Empty, err
	}

	utxos := make([][]byte, len(res.UTXOs))
	for i, utxo := range res.UTXOs {
		utxoBytes, err := formatting.Decode(res.Encoding, utxo)
		if err != nil {
			return nil, ids.ShortID{}, ids.Empty, err
		}
		utxos[i] = utxoBytes
	}
	endAddr, err := address.ParseToID(res.EndIndex.Address)
	if err != nil {
		return nil, ids.ShortID{}, ids.Empty, err
	}
	endUTXOID, err := ids.FromString(res.EndIndex.UTXO)
	return utxos, endAddr, endUTXOID, err
}

// GetAssetDescription returns a description of assetID.
func (c *Client) GetAssetDescription(ctx context.Context, assetID string, options ...rpc.Option) (*GetAssetDescriptionReply, error) {
	res := &GetAssetDescriptionReply{}
	err := c.Requester.SendRequest(ctx, "avm.getAssetDescription", &GetAssetDescriptionArgs{
		AssetID: assetID,
	}, res, options...)
	return res, err
}

// GetBalance returns the balance of assetID held by addr.
//
// If includePartial is set, balance includes partial owned (i.e. in a multisig)
// funds.
//
// Deprecated: GetUTXOs should be used instead.
func (c *Client) GetBalance(
	ctx context.Context,
	addr ids.ShortID,
	assetID string,
	includePartial bool,
	options ...rpc.Option,
) (*GetBalanceReply, error) {
	res := &GetBalanceReply{}
	err := c.Requester.SendRequest(ctx, "avm.getBalance", &GetBalanceArgs{
		Address:        addr.String(),
		AssetID:        assetID,
		IncludePartial: includePartial,
	}, res, options...)
	return res, err
}

// GetAllBalances returns all asset balances for addr.
//
// Deprecated: GetUTXOs should be used instead.
func (c *Client) GetAllBalances(
	ctx context.Context,
	addr ids.ShortID,
	includePartial bool,
	options ...rpc.Option,
) ([]Balance, error) {
	res := &GetAllBalancesReply{}
	err := c.Requester.SendRequest(ctx, "avm.getAllBalances", &GetAllBalancesArgs{
		JSONAddress:    api.JSONAddress{Address: addr.String()},
		IncludePartial: includePartial,
	}, res, options...)
	return res.Balances, err
}

// GetTxFee returns the cost to issue certain transactions.
func (c *Client) GetTxFee(ctx context.Context, options ...rpc.Option) (uint64, uint64, error) {
	res := &GetTxFeeReply{}
	err := c.Requester.SendRequest(ctx, "avm.getTxFee", struct{}{}, res, options...)
	return uint64(res.TxFee), uint64(res.CreateAssetTxFee), err
}

func AwaitTxAccepted(
	c *Client,
	ctx context.Context,
	txID ids.ID,
	freq time.Duration,
	options ...rpc.Option,
) error {
	ticker := time.NewTicker(freq)
	defer ticker.Stop()

	for {
		status, err := c.GetTxStatus(ctx, txID, options...)
		if err != nil {
			return err
		}

		switch status {
		case choices.Accepted:
			return nil
		case choices.Rejected:
			return ErrRejected
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
