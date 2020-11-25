// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	cjson "github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// Client ...
type Client struct {
	requester rpc.EndpointRequester
}

// NewClient returns a Client for interacting with EVM [chain]
func NewClient(uri, chain string, requestTimeout time.Duration) *Client {
	return &Client{
		requester: rpc.NewEndpointRequester(uri, fmt.Sprintf("/ext/bc/%s/avax", chain), "avax", requestTimeout),
	}
}

// NewCChainClient returns a Client for interacting with the C Chain
func NewCChainClient(uri string, requestTimeout time.Duration) *Client {
	return NewClient(uri, "C", requestTimeout)
}

// IssueTx issues a transaction to a node and returns the TxID
func (c *Client) IssueTx(txBytes []byte) (ids.ID, error) {
	res := &api.JSONTxID{}
	txStr, err := formatting.Encode(formatting.Hex, txBytes)
	if err != nil {
		return res.TxID, fmt.Errorf("problem hex encoding bytes: %w", err)
	}
	err = c.requester.SendRequest("issueTx", &api.FormattedTx{
		Tx:       txStr,
		Encoding: formatting.Hex,
	}, res)
	return res.TxID, err
}

// GetTxStatus returns the status of [txID]
// func (c *Client) GetTxStatus(txID ids.ID) (choices.Status, error) {
// 	res := &GetTxStatusReply{}
// 	err := c.requester.SendRequest("getTxStatus", &api.JSONTxID{
// 		TxID: txID,
// 	}, res)
// 	return res.Status, err
// }

// GetTx returns the byte representation of [txID]
func (c *Client) GetTx(txID ids.ID) ([]byte, error) {
	res := &api.FormattedTx{}
	err := c.requester.SendRequest("getTx", &api.GetTxArgs{
		TxID:     txID,
		Encoding: formatting.Hex,
	}, res)
	if err != nil {
		return nil, err
	}

	return formatting.Decode(formatting.Hex, res.Tx)
}

// GetUTXOs returns the byte representation of the UTXOs controlled by [addrs]
func (c *Client) GetUTXOs(addrs []string, limit uint32, startAddress, startUTXOID string) ([][]byte, api.Index, error) {
	res := &api.GetUTXOsReply{}
	err := c.requester.SendRequest("getUTXOs", &api.GetUTXOsArgs{
		Addresses: addrs,
		Limit:     cjson.Uint32(limit),
		StartIndex: api.Index{
			Address: startAddress,
			UTXO:    startUTXOID,
		},
		Encoding: formatting.Hex,
	}, res)
	if err != nil {
		return nil, api.Index{}, err
	}

	utxos := make([][]byte, len(res.UTXOs))
	for i, utxo := range res.UTXOs {
		b, err := formatting.Decode(formatting.Hex, utxo)
		if err != nil {
			return nil, api.Index{}, err
		}
		utxos[i] = b
	}
	return utxos, res.EndIndex, nil
}

// CreateAddress creates a new address controlled by [user]
func (c *Client) CreateAddress(user api.UserPass) (string, error) {
	res := &api.JSONAddress{}
	err := c.requester.SendRequest("createAddress", &user, res)
	return res.Address, err
}

// ListAddresses returns all addresses on this chain controlled by [user]
func (c *Client) ListAddresses(user api.UserPass) ([]string, error) {
	res := &api.JSONAddresses{}
	err := c.requester.SendRequest("listAddresses", &user, res)
	return res.Addresses, err
}

// ExportKey returns the private key corresponding to [addr] controlled by [user]
// in both Avalanche standard format and hex format
func (c *Client) ExportKey(user api.UserPass, addr string) (string, string, error) {
	res := &ExportKeyReply{}
	err := c.requester.SendRequest("exportKey", &ExportKeyArgs{
		UserPass: user,
		Address:  addr,
	}, res)
	return res.PrivateKey, res.PrivateKeyHex, err
}

// ImportKey imports [privateKey] to [user]
func (c *Client) ImportKey(user api.UserPass, privateKey string) (string, error) {
	res := &api.JSONAddress{}
	err := c.requester.SendRequest("importKey", &ImportKeyArgs{
		UserPass:   user,
		PrivateKey: privateKey,
	}, res)
	return res.Address, err
}

// Import sends an import transaction to import funds from [sourceChain] and
// returns the ID of the newly created transaction
func (c *Client) Import(user api.UserPass, to, sourceChain string) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest("import", &ImportArgs{
		UserPass:    user,
		To:          to,
		SourceChain: sourceChain,
	}, res)
	return res.TxID, err
}

// ExportAVAX sends AVAX from this chain to the address specified by [to].
// Returns the ID of the newly created atomic transaction
func (c *Client) ExportAVAX(
	user api.UserPass,
	amount uint64,
	to string,
) (ids.ID, error) {
	return c.Export(user, amount, to, "AVAX")
}

// Export sends an asset from this chain to the P/C-Chain.
// After this tx is accepted, the AVAX must be imported to the P/C-chain with an importTx.
// Returns the ID of the newly created atomic transaction
func (c *Client) Export(
	user api.UserPass,
	amount uint64,
	to string,
	assetID string,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest("export", &ExportArgs{
		ExportAVAXArgs: ExportAVAXArgs{
			UserPass: user,
			Amount:   cjson.Uint64(amount),
			To:       to,
		},
		AssetID: assetID,
	}, res)
	return res.TxID, err
}
