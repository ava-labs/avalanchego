// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/rpc"

	cjson "github.com/ava-labs/avalanchego/utils/json"
)

// Interface compliance
var _ Client = &client{}

// Client for interacting with an AVM (X-Chain) instance
type Client interface {
	WalletClient
	// GetTxStatus returns the status of [txID]
	GetTxStatus(txID ids.ID) (choices.Status, error)
	// ConfirmTx attempts to confirm [txID] by checking its status [numChecks] times
	// with [checkFreq] between each attempt. If the transaction has not been decided
	// by the final attempt, it returns the status of the last attempt.
	// Note: ConfirmTx will block until either the last attempt finishes or the client
	// returns a decided status.
	ConfirmTx(txID ids.ID, numChecks int, checkFreq time.Duration) (choices.Status, error)
	// GetTx returns the byte representation of [txID]
	GetTx(txID ids.ID) ([]byte, error)
	// GetUTXOs returns the byte representation of the UTXOs controlled by [addrs]
	GetUTXOs(
		addrs []string,
		limit uint32,
		startAddress,
		startUTXOID string,
	) ([][]byte, api.Index, error)
	// GetAtomicUTXOs returns the byte representation of the atomic UTXOs controlled by [addresses]
	// from [sourceChain]
	GetAtomicUTXOs(
		addrs []string,
		sourceChain string,
		limit uint32,
		startAddress,
		startUTXOID string,
	) ([][]byte, api.Index, error)
	// GetAssetDescription returns a description of [assetID]
	GetAssetDescription(assetID string) (*GetAssetDescriptionReply, error)
	// GetBalance returns the balance of [assetID] held by [addr].
	// If [includePartial], balance includes partial owned (i.e. in a multisig) funds.
	GetBalance(addr string, assetID string, includePartial bool) (*GetBalanceReply, error)
	// GetAllBalances returns all asset balances for [addr]
	// CreateAsset creates a new asset and returns its assetID
	GetAllBalances(string, bool) (*GetAllBalancesReply, error)
	CreateAsset(
		user api.UserPass,
		from []string,
		changeAddr,
		name,
		symbol string,
		denomination byte,
		holders []*Holder,
		minters []Owners,
	) (ids.ID, error)
	// CreateFixedCapAsset creates a new fixed cap asset and returns its assetID
	CreateFixedCapAsset(
		user api.UserPass,
		from []string,
		changeAddr,
		name,
		symbol string,
		denomination byte,
		holders []*Holder,
	) (ids.ID, error)
	// CreateVariableCapAsset creates a new variable cap asset and returns its assetID
	CreateVariableCapAsset(
		user api.UserPass,
		from []string,
		changeAddr,
		name,
		symbol string,
		denomination byte,
		minters []Owners,
	) (ids.ID, error)
	// CreateNFTAsset creates a new NFT asset and returns its assetID
	CreateNFTAsset(
		user api.UserPass,
		from []string,
		changeAddr,
		name,
		symbol string,
		minters []Owners,
	) (ids.ID, error)
	// CreateAddress creates a new address controlled by [user]
	CreateAddress(user api.UserPass) (string, error)
	// ListAddresses returns all addresses on this chain controlled by [user]
	ListAddresses(user api.UserPass) ([]string, error)
	// ExportKey returns the private key corresponding to [addr] controlled by [user]
	ExportKey(user api.UserPass, addr string) (string, error)
	// ImportKey imports [privateKey] to [user]
	ImportKey(user api.UserPass, privateKey string) (string, error)
	// Mint [amount] of [assetID] to be owned by [to]
	Mint(
		user api.UserPass,
		from []string,
		changeAddr string,
		amount uint64,
		assetID,
		to string,
	) (ids.ID, error)
	// SendNFT sends an NFT and returns the ID of the newly created transaction
	SendNFT(
		user api.UserPass,
		from []string,
		changeAddr string,
		assetID string,
		groupID uint32,
		to string,
	) (ids.ID, error)
	// MintNFT issues a MintNFT transaction and returns the ID of the newly created transaction
	MintNFT(
		user api.UserPass,
		from []string,
		changeAddr string,
		assetID string,
		payload []byte,
		to string,
	) (ids.ID, error)
	// Import sends an import transaction to import funds from [sourceChain] and
	// returns the ID of the newly created transaction
	Import(user api.UserPass, to, sourceChain string) (ids.ID, error) // Export sends an asset from this chain to the P/C-Chain.
	// After this tx is accepted, the AVAX must be imported to the P/C-chain with an importTx.
	// Returns the ID of the newly created atomic transaction
	Export(
		user api.UserPass,
		from []string,
		changeAddr string,
		amount uint64,
		to string,
		assetID string,
	) (ids.ID, error)
}

// implementation for an AVM client for interacting with avm [chain]
type client struct {
	requester rpc.EndpointRequester
}

// NewClient returns an AVM client for interacting with avm [chain]
func NewClient(uri, chain string, requestTimeout time.Duration) Client {
	return &client{
		requester: rpc.NewEndpointRequester(uri, fmt.Sprintf("/ext/%s", constants.ChainAliasPrefix+chain), "avm", requestTimeout),
	}
}

func (c *client) IssueTx(txBytes []byte) (ids.ID, error) {
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

func (c *client) GetTxStatus(txID ids.ID) (choices.Status, error) {
	res := &GetTxStatusReply{}
	err := c.requester.SendRequest("getTxStatus", &api.JSONTxID{
		TxID: txID,
	}, res)
	return res.Status, err
}

func (c *client) ConfirmTx(txID ids.ID, attempts int, delay time.Duration) (choices.Status, error) {
	for i := 0; i < attempts-1; i++ {
		status, err := c.GetTxStatus(txID)
		if err != nil {
			return status, err
		}

		if status.Decided() {
			return status, nil
		}
		time.Sleep(delay)
	}

	return c.GetTxStatus(txID)
}

func (c *client) GetTx(txID ids.ID) ([]byte, error) {
	res := &api.FormattedTx{}
	err := c.requester.SendRequest("getTx", &api.GetTxArgs{
		TxID:     txID,
		Encoding: formatting.Hex,
	}, res)
	if err != nil {
		return nil, err
	}

	txBytes, err := formatting.Decode(res.Encoding, res.Tx)
	if err != nil {
		return nil, err
	}
	return txBytes, nil
}

func (c *client) GetUTXOs(addrs []string, limit uint32, startAddress, startUTXOID string) ([][]byte, api.Index, error) {
	return c.GetAtomicUTXOs(addrs, "", limit, startAddress, startUTXOID)
}

func (c *client) GetAtomicUTXOs(addrs []string, sourceChain string, limit uint32, startAddress, startUTXOID string) ([][]byte, api.Index, error) {
	res := &api.GetUTXOsReply{}
	err := c.requester.SendRequest("getUTXOs", &api.GetUTXOsArgs{
		Addresses:   addrs,
		SourceChain: sourceChain,
		Limit:       cjson.Uint32(limit),
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
		utxoBytes, err := formatting.Decode(res.Encoding, utxo)
		if err != nil {
			return nil, api.Index{}, err
		}
		utxos[i] = utxoBytes
	}
	return utxos, res.EndIndex, nil
}

func (c *client) GetAssetDescription(assetID string) (*GetAssetDescriptionReply, error) {
	res := &GetAssetDescriptionReply{}
	err := c.requester.SendRequest("getAssetDescription", &GetAssetDescriptionArgs{
		AssetID: assetID,
	}, res)
	return res, err
}

func (c *client) GetBalance(addr string, assetID string, includePartial bool) (*GetBalanceReply, error) {
	res := &GetBalanceReply{}
	err := c.requester.SendRequest("getBalance", &GetBalanceArgs{
		Address:        addr,
		AssetID:        assetID,
		IncludePartial: includePartial,
	}, res)
	return res, err
}

func (c *client) GetAllBalances(addr string, includePartial bool) (*GetAllBalancesReply, error) {
	res := &GetAllBalancesReply{}
	err := c.requester.SendRequest("getAllBalances", &GetAllBalancesArgs{
		JSONAddress:    api.JSONAddress{Address: addr},
		IncludePartial: includePartial,
	}, res)
	return res, err
}

func (c *client) CreateAsset(
	user api.UserPass,
	from []string,
	changeAddr,
	name,
	symbol string,
	denomination byte,
	holders []*Holder,
	minters []Owners,
) (ids.ID, error) {
	res := &FormattedAssetID{}
	err := c.requester.SendRequest("createAsset", &CreateAssetArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		Name:           name,
		Symbol:         symbol,
		Denomination:   denomination,
		InitialHolders: holders,
		MinterSets:     minters,
	}, res)
	return res.AssetID, err
}

func (c *client) CreateFixedCapAsset(
	user api.UserPass,
	from []string,
	changeAddr,
	name,
	symbol string,
	denomination byte,
	holders []*Holder,
) (ids.ID, error) {
	res := &FormattedAssetID{}
	err := c.requester.SendRequest("createAsset", &CreateAssetArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		Name:           name,
		Symbol:         symbol,
		Denomination:   denomination,
		InitialHolders: holders,
	}, res)
	return res.AssetID, err
}

func (c *client) CreateVariableCapAsset(
	user api.UserPass,
	from []string,
	changeAddr,
	name,
	symbol string,
	denomination byte,
	minters []Owners,
) (ids.ID, error) {
	res := &FormattedAssetID{}
	err := c.requester.SendRequest("createAsset", &CreateAssetArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		Name:         name,
		Symbol:       symbol,
		Denomination: denomination,
		MinterSets:   minters,
	}, res)
	return res.AssetID, err
}

func (c *client) CreateNFTAsset(
	user api.UserPass,
	from []string,
	changeAddr,
	name,
	symbol string,
	minters []Owners,
) (ids.ID, error) {
	res := &FormattedAssetID{}
	err := c.requester.SendRequest("createNFTAsset", &CreateNFTAssetArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		Name:       name,
		Symbol:     symbol,
		MinterSets: minters,
	}, res)
	return res.AssetID, err
}

func (c *client) CreateAddress(user api.UserPass) (string, error) {
	res := &api.JSONAddress{}
	err := c.requester.SendRequest("createAddress", &user, res)
	return res.Address, err
}

func (c *client) ListAddresses(user api.UserPass) ([]string, error) {
	res := &api.JSONAddresses{}
	err := c.requester.SendRequest("listAddresses", &user, res)
	return res.Addresses, err
}

func (c *client) ExportKey(user api.UserPass, addr string) (string, error) {
	res := &ExportKeyReply{}
	err := c.requester.SendRequest("exportKey", &ExportKeyArgs{
		UserPass: user,
		Address:  addr,
	}, res)
	return res.PrivateKey, err
}

func (c *client) ImportKey(user api.UserPass, privateKey string) (string, error) {
	res := &api.JSONAddress{}
	err := c.requester.SendRequest("importKey", &ImportKeyArgs{
		UserPass:   user,
		PrivateKey: privateKey,
	}, res)
	return res.Address, err
}

func (c *client) Send(
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

func (c *client) SendMultiple(
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

func (c *client) Mint(
	user api.UserPass,
	from []string,
	changeAddr string,
	amount uint64,
	assetID,
	to string,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest("mint", &MintArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		Amount:  cjson.Uint64(amount),
		AssetID: assetID,
		To:      to,
	}, res)
	return res.TxID, err
}

func (c *client) SendNFT(
	user api.UserPass,
	from []string,
	changeAddr string,
	assetID string,
	groupID uint32,
	to string,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest("sendNFT", &SendNFTArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		AssetID: assetID,
		GroupID: cjson.Uint32(groupID),
		To:      to,
	}, res)
	return res.TxID, err
}

func (c *client) MintNFT(
	user api.UserPass,
	from []string,
	changeAddr string,
	assetID string,
	payload []byte,
	to string,
) (ids.ID, error) {
	payloadStr, err := formatting.EncodeWithChecksum(formatting.Hex, payload)
	if err != nil {
		return ids.ID{}, err
	}
	res := &api.JSONTxID{}
	err = c.requester.SendRequest("mintNFT", &MintNFTArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		AssetID:  assetID,
		Payload:  payloadStr,
		To:       to,
		Encoding: formatting.Hex,
	}, res)
	return res.TxID, err
}

func (c *client) Import(user api.UserPass, to, sourceChain string) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest("import", &ImportArgs{
		UserPass:    user,
		To:          to,
		SourceChain: sourceChain,
	}, res)
	return res.TxID, err
}

func (c *client) Export(
	user api.UserPass,
	from []string,
	changeAddr string,
	amount uint64,
	to string,
	assetID string,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest("export", &ExportArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		Amount:  cjson.Uint64(amount),
		To:      to,
		AssetID: assetID,
	}, res)
	return res.TxID, err
}
