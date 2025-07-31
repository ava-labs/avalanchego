// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/block"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/genesis"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/tx"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

const DefaultPollingInterval = 50 * time.Millisecond

func NewClient(uri, chain string) *Client {
	path := fmt.Sprintf(
		"%s/ext/%s/%s",
		uri,
		constants.ChainAliasPrefix,
		chain,
	)
	return &Client{
		Req: rpc.NewEndpointRequester(path),
	}
}

type Client struct {
	Req rpc.EndpointRequester
}

func (c *Client) Network(
	ctx context.Context,
	options ...rpc.Option,
) (uint32, ids.ID, ids.ID, error) {
	resp := new(NetworkReply)
	err := c.Req.SendRequest(
		ctx,
		"xsvm.network",
		nil,
		resp,
		options...,
	)
	return resp.NetworkID, resp.SubnetID, resp.ChainID, err
}

func (c *Client) Genesis(
	ctx context.Context,
	options ...rpc.Option,
) (*genesis.Genesis, error) {
	resp := new(GenesisReply)
	err := c.Req.SendRequest(
		ctx,
		"xsvm.genesis",
		nil,
		resp,
		options...,
	)
	return resp.Genesis, err
}

func (c *Client) Nonce(
	ctx context.Context,
	address ids.ShortID,
	options ...rpc.Option,
) (uint64, error) {
	resp := new(NonceReply)
	err := c.Req.SendRequest(
		ctx,
		"xsvm.nonce",
		&NonceArgs{
			Address: address,
		},
		resp,
		options...,
	)
	return resp.Nonce, err
}

func (c *Client) Balance(
	ctx context.Context,
	address ids.ShortID,
	assetID ids.ID,
	options ...rpc.Option,
) (uint64, error) {
	resp := new(BalanceReply)
	err := c.Req.SendRequest(
		ctx,
		"xsvm.balance",
		&BalanceArgs{
			Address: address,
			AssetID: assetID,
		},
		resp,
		options...,
	)
	return resp.Balance, err
}

func (c *Client) Loan(
	ctx context.Context,
	chainID ids.ID,
	options ...rpc.Option,
) (uint64, error) {
	resp := new(LoanReply)
	err := c.Req.SendRequest(
		ctx,
		"xsvm.loan",
		&LoanArgs{
			ChainID: chainID,
		},
		resp,
		options...,
	)
	return resp.Amount, err
}

func (c *Client) IssueTx(
	ctx context.Context,
	newTx *tx.Tx,
	options ...rpc.Option,
) (ids.ID, error) {
	txBytes, err := tx.Codec.Marshal(tx.CodecVersion, newTx)
	if err != nil {
		return ids.Empty, err
	}

	resp := new(IssueTxReply)
	err = c.Req.SendRequest(
		ctx,
		"xsvm.issueTx",
		&IssueTxArgs{
			Tx: txBytes,
		},
		resp,
		options...,
	)
	return resp.TxID, err
}

func (c *Client) LastAccepted(
	ctx context.Context,
	options ...rpc.Option,
) (ids.ID, *block.Stateless, error) {
	resp := new(LastAcceptedReply)
	err := c.Req.SendRequest(
		ctx,
		"xsvm.lastAccepted",
		nil,
		resp,
		options...,
	)
	return resp.BlockID, resp.Block, err
}

func (c *Client) Block(
	ctx context.Context,
	blkID ids.ID,
	options ...rpc.Option,
) (*block.Stateless, error) {
	resp := new(BlockReply)
	err := c.Req.SendRequest(
		ctx,
		"xsvm.lastAccepted",
		&BlockArgs{
			BlockID: blkID,
		},
		resp,
		options...,
	)
	return resp.Block, err
}

func (c *Client) Message(
	ctx context.Context,
	txID ids.ID,
	options ...rpc.Option,
) (*warp.UnsignedMessage, []byte, error) {
	resp := new(MessageReply)
	err := c.Req.SendRequest(
		ctx,
		"xsvm.message",
		&MessageArgs{
			TxID: txID,
		},
		resp,
		options...,
	)
	if err != nil {
		return nil, nil, err
	}
	return resp.Message, resp.Signature, resp.Message.Initialize()
}

func AwaitTxAccepted(
	ctx context.Context,
	c *Client,
	address ids.ShortID,
	nonce uint64,
	freq time.Duration,
	options ...rpc.Option,
) error {
	ticker := time.NewTicker(freq)
	defer ticker.Stop()

	for {
		currentNonce, err := c.Nonce(ctx, address, options...)
		if err != nil {
			return err
		}

		if currentNonce > nonce {
			// The nonce increasing indicates the acceptance of a transaction
			// issued with the specified nonce.
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
