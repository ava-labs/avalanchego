// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// WalletClient for interacting with avm managed wallet.
//
// Deprecated: Transactions should be issued using the
// `avalanchego/wallet/chain/x.Wallet` utility.
type WalletClient struct {
	Requester rpc.EndpointRequester
}

// NewWalletClient returns an AVM wallet client for interacting with avm managed
// wallet
//
// Deprecated: Transactions should be issued using the
// `avalanchego/wallet/chain/x.Wallet` utility.
func NewWalletClient(uri, chain string) *WalletClient {
	path := fmt.Sprintf(
		"%s/ext/%s/%s/wallet",
		uri,
		constants.ChainAliasPrefix,
		chain,
	)
	return &WalletClient{
		Requester: rpc.NewEndpointRequester(path),
	}
}

// IssueTx issues a transaction to a node and returns the TxID
func (c *WalletClient) IssueTx(ctx context.Context, txBytes []byte, options ...rpc.Option) (ids.ID, error) {
	txStr, err := formatting.Encode(formatting.Hex, txBytes)
	if err != nil {
		return ids.Empty, err
	}
	res := &api.JSONTxID{}
	err = c.Requester.SendRequest(ctx, "wallet.issueTx", &api.FormattedTx{
		Tx:       txStr,
		Encoding: formatting.Hex,
	}, res, options...)
	return res.TxID, err
}
