// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	cjson "github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// WalletClient ...
type WalletClient struct {
	requester rpc.EndpointRequester
}

// NewWalletClient returns an AVM wallet client for interacting with avm managed wallet on [chain]
func NewWalletClient(uri, chain string, requestTimeout time.Duration) *WalletClient {
	return &WalletClient{
		requester: rpc.NewEndpointRequester(uri, fmt.Sprintf("/ext/bc/%s/wallet", chain), "wallet", requestTimeout),
	}
}

// IssueTx issues a transaction to a node and returns the TxID
func (c *WalletClient) IssueTx(txBytes []byte) (ids.ID, error) {
	txStr, err := formatting.Encode(formatting.Hex, txBytes)
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

// Send [amount] of [assetID] to address [to]
func (c *WalletClient) Send(
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

// SendMultiple sends a transaction from [user] funding all [outputs]
func (c *WalletClient) SendMultiple(
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
