// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	cjson "github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// Interface compliance
var _ WalletClient = &client{}

// interface of an AVM wallet client for interacting with avm managed wallet on [chain]
type WalletClient interface {
	// IssueTx issues a transaction to a node and returns the TxID
	IssueTx(tx []byte) (ids.ID, error)
	// Send [amount] of [assetID] to address [to]
	Send(
		user api.UserPass,
		from []string,
		changeAddr string,
		amount uint64,
		assetID,
		to,
		memo string,
	) (ids.ID, error)
	// SendMultiple sends a transaction from [user] funding all [outputs]
	SendMultiple(
		user api.UserPass,
		from []string,
		changeAddr string,
		outputs []SendOutput,
		memo string,
	) (ids.ID, error)
}

// implementation of an AVM wallet client for interacting with avm managed wallet on [chain]
type walletClient struct {
	requester rpc.EndpointRequester
}

// NewWalletClient returns an AVM wallet client for interacting with avm managed wallet on [chain]
func NewWalletClient(uri, chain string, requestTimeout time.Duration) WalletClient {
	return &walletClient{
		requester: rpc.NewEndpointRequester(uri, fmt.Sprintf("/ext/%s/wallet", constants.ChainAliasPrefix+chain), "wallet", requestTimeout),
	}
}

func (c *walletClient) IssueTx(txBytes []byte) (ids.ID, error) {
	txStr, err := formatting.EncodeWithChecksum(formatting.Hex, txBytes)
	if err != nil {
		return ids.ID{}, err
	}
	res := &api.JSONTxID{}
	err = c.requester.SendRequest("issueTx", &api.FormattedTx{
		Tx:       txStr,
		Encoding: formatting.Hex,
	}, res)
	return res.TxID, err
}

func (c *walletClient) Send(
	user api.UserPass,
	from []string,
	changeAddr string,
	amount uint64,
	assetID,
	to,
	memo string,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest("send", &SendArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		SendOutput: SendOutput{
			Amount:  cjson.Uint64(amount),
			AssetID: assetID,
			To:      to,
		},
		Memo: memo,
	}, res)
	return res.TxID, err
}

func (c *walletClient) SendMultiple(
	user api.UserPass,
	from []string,
	changeAddr string,
	outputs []SendOutput,
	memo string,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest("sendMultiple", &SendMultipleArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		Outputs: outputs,
		Memo:    memo,
	}, res)
	return res.TxID, err
}
