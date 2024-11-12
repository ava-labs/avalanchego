// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

var (
	_ Client = (*client)(nil)

	ErrRejected = errors.New("rejected")
)

// Client for interacting with an AVM (X-Chain) instance
type Client interface {
	WalletClient
	// GetBlock returns the block with the given id.
	GetBlock(ctx context.Context, blkID ids.ID, options ...rpc.Option) ([]byte, error)
	// GetBlockByHeight returns the block at the given [height].
	GetBlockByHeight(ctx context.Context, height uint64, options ...rpc.Option) ([]byte, error)
	// GetHeight returns the height of the last accepted block.
	GetHeight(ctx context.Context, options ...rpc.Option) (uint64, error)
	// GetTxStatus returns the status of [txID]
	//
	// Deprecated: GetTxStatus only returns Accepted or Unknown, GetTx should be
	// used instead to determine if the tx was accepted.
	GetTxStatus(ctx context.Context, txID ids.ID, options ...rpc.Option) (choices.Status, error)
	// GetTx returns the byte representation of [txID]
	GetTx(ctx context.Context, txID ids.ID, options ...rpc.Option) ([]byte, error)
	// GetUTXOs returns the byte representation of the UTXOs controlled by [addrs]
	GetUTXOs(
		ctx context.Context,
		addrs []ids.ShortID,
		limit uint32,
		startAddress ids.ShortID,
		startUTXOID ids.ID,
		options ...rpc.Option,
	) ([][]byte, ids.ShortID, ids.ID, error)
	// GetAtomicUTXOs returns the byte representation of the atomic UTXOs controlled by [addrs]
	// from [sourceChain]
	GetAtomicUTXOs(
		ctx context.Context,
		addrs []ids.ShortID,
		sourceChain string,
		limit uint32,
		startAddress ids.ShortID,
		startUTXOID ids.ID,
		options ...rpc.Option,
	) ([][]byte, ids.ShortID, ids.ID, error)
	// GetAssetDescription returns a description of [assetID]
	GetAssetDescription(ctx context.Context, assetID string, options ...rpc.Option) (*GetAssetDescriptionReply, error)
	// GetBalance returns the balance of [assetID] held by [addr].
	// If [includePartial], balance includes partial owned (i.e. in a multisig) funds.
	//
	// Deprecated: GetUTXOs should be used instead.
	GetBalance(ctx context.Context, addr ids.ShortID, assetID string, includePartial bool, options ...rpc.Option) (*GetBalanceReply, error)
	// GetAllBalances returns all asset balances for [addr]
	//
	// Deprecated: GetUTXOs should be used instead.
	GetAllBalances(ctx context.Context, addr ids.ShortID, includePartial bool, options ...rpc.Option) ([]Balance, error)
	// CreateAsset creates a new asset and returns its assetID
	//
	// Deprecated: Transactions should be issued using the
	// `avalanchego/wallet/chain/x.Wallet` utility.
	CreateAsset(
		ctx context.Context,
		user api.UserPass,
		from []ids.ShortID,
		changeAddr ids.ShortID,
		name string,
		symbol string,
		denomination byte,
		holders []*ClientHolder,
		minters []ClientOwners,
		options ...rpc.Option,
	) (ids.ID, error)
	// CreateFixedCapAsset creates a new fixed cap asset and returns its assetID
	//
	// Deprecated: Transactions should be issued using the
	// `avalanchego/wallet/chain/x.Wallet` utility.
	CreateFixedCapAsset(
		ctx context.Context,
		user api.UserPass,
		from []ids.ShortID,
		changeAddr ids.ShortID,
		name string,
		symbol string,
		denomination byte,
		holders []*ClientHolder,
		options ...rpc.Option,
	) (ids.ID, error)
	// CreateVariableCapAsset creates a new variable cap asset and returns its assetID
	//
	// Deprecated: Transactions should be issued using the
	// `avalanchego/wallet/chain/x.Wallet` utility.
	CreateVariableCapAsset(
		ctx context.Context,
		user api.UserPass,
		from []ids.ShortID,
		changeAddr ids.ShortID,
		name string,
		symbol string,
		denomination byte,
		minters []ClientOwners,
		options ...rpc.Option,
	) (ids.ID, error)
	// CreateNFTAsset creates a new NFT asset and returns its assetID
	//
	// Deprecated: Transactions should be issued using the
	// `avalanchego/wallet/chain/x.Wallet` utility.
	CreateNFTAsset(
		ctx context.Context,
		user api.UserPass,
		from []ids.ShortID,
		changeAddr ids.ShortID,
		name string,
		symbol string,
		minters []ClientOwners,
		options ...rpc.Option,
	) (ids.ID, error)
	// CreateAddress creates a new address controlled by [user]
	//
	// Deprecated: Keys should no longer be stored on the node.
	CreateAddress(ctx context.Context, user api.UserPass, options ...rpc.Option) (ids.ShortID, error)
	// ListAddresses returns all addresses on this chain controlled by [user]
	//
	// Deprecated: Keys should no longer be stored on the node.
	ListAddresses(ctx context.Context, user api.UserPass, options ...rpc.Option) ([]ids.ShortID, error)
	// ExportKey returns the private key corresponding to [addr] controlled by [user]
	//
	// Deprecated: Keys should no longer be stored on the node.
	ExportKey(ctx context.Context, user api.UserPass, addr ids.ShortID, options ...rpc.Option) (*secp256k1.PrivateKey, error)
	// ImportKey imports [privateKey] to [user]
	//
	// Deprecated: Keys should no longer be stored on the node.
	ImportKey(ctx context.Context, user api.UserPass, privateKey *secp256k1.PrivateKey, options ...rpc.Option) (ids.ShortID, error)
	// Mint [amount] of [assetID] to be owned by [to]
	//
	// Deprecated: Transactions should be issued using the
	// `avalanchego/wallet/chain/x.Wallet` utility.
	Mint(
		ctx context.Context,
		user api.UserPass,
		from []ids.ShortID,
		changeAddr ids.ShortID,
		amount uint64,
		assetID string,
		to ids.ShortID,
		options ...rpc.Option,
	) (ids.ID, error)
	// SendNFT sends an NFT and returns the ID of the newly created transaction
	//
	// Deprecated: Transactions should be issued using the
	// `avalanchego/wallet/chain/x.Wallet` utility.
	SendNFT(
		ctx context.Context,
		user api.UserPass,
		from []ids.ShortID,
		changeAddr ids.ShortID,
		assetID string,
		groupID uint32,
		to ids.ShortID,
		options ...rpc.Option,
	) (ids.ID, error)
	// MintNFT issues a MintNFT transaction and returns the ID of the newly created transaction
	//
	// Deprecated: Transactions should be issued using the
	// `avalanchego/wallet/chain/x.Wallet` utility.
	MintNFT(
		ctx context.Context,
		user api.UserPass,
		from []ids.ShortID,
		changeAddr ids.ShortID,
		assetID string,
		payload []byte,
		to ids.ShortID,
		options ...rpc.Option,
	) (ids.ID, error)
	// Import sends an import transaction to import funds from [sourceChain] and
	// returns the ID of the newly created transaction
	//
	// Deprecated: Transactions should be issued using the
	// `avalanchego/wallet/chain/x.Wallet` utility.
	Import(ctx context.Context, user api.UserPass, to ids.ShortID, sourceChain string, options ...rpc.Option) (ids.ID, error) // Export sends an asset from this chain to the P/C-Chain.
	// After this tx is accepted, the AVAX must be imported to the P/C-chain with an importTx.
	// Returns the ID of the newly created atomic transaction
	//
	// Deprecated: Transactions should be issued using the
	// `avalanchego/wallet/chain/x.Wallet` utility.
	Export(
		ctx context.Context,
		user api.UserPass,
		from []ids.ShortID,
		changeAddr ids.ShortID,
		amount uint64,
		to ids.ShortID,
		toChainIDAlias string,
		assetID string,
		options ...rpc.Option,
	) (ids.ID, error)
}

// implementation for an AVM client for interacting with avm [chain]
type client struct {
	requester rpc.EndpointRequester
}

// NewClient returns an AVM client for interacting with avm [chain]
func NewClient(uri, chain string) Client {
	path := fmt.Sprintf(
		"%s/ext/%s/%s",
		uri,
		constants.ChainAliasPrefix,
		chain,
	)
	return &client{
		requester: rpc.NewEndpointRequester(path),
	}
}

func (c *client) GetBlock(ctx context.Context, blkID ids.ID, options ...rpc.Option) ([]byte, error) {
	res := &api.FormattedBlock{}
	err := c.requester.SendRequest(ctx, "avm.getBlock", &api.GetBlockArgs{
		BlockID:  blkID,
		Encoding: formatting.HexNC,
	}, res, options...)
	if err != nil {
		return nil, err
	}
	return formatting.Decode(res.Encoding, res.Block)
}

func (c *client) GetBlockByHeight(ctx context.Context, height uint64, options ...rpc.Option) ([]byte, error) {
	res := &api.FormattedBlock{}
	err := c.requester.SendRequest(ctx, "avm.getBlockByHeight", &api.GetBlockByHeightArgs{
		Height:   json.Uint64(height),
		Encoding: formatting.HexNC,
	}, res, options...)
	if err != nil {
		return nil, err
	}
	return formatting.Decode(res.Encoding, res.Block)
}

func (c *client) GetHeight(ctx context.Context, options ...rpc.Option) (uint64, error) {
	res := &api.GetHeightResponse{}
	err := c.requester.SendRequest(ctx, "avm.getHeight", struct{}{}, res, options...)
	return uint64(res.Height), err
}

func (c *client) IssueTx(ctx context.Context, txBytes []byte, options ...rpc.Option) (ids.ID, error) {
	txStr, err := formatting.Encode(formatting.Hex, txBytes)
	if err != nil {
		return ids.Empty, err
	}
	res := &api.JSONTxID{}
	err = c.requester.SendRequest(ctx, "avm.issueTx", &api.FormattedTx{
		Tx:       txStr,
		Encoding: formatting.Hex,
	}, res, options...)
	return res.TxID, err
}

func (c *client) GetTxStatus(ctx context.Context, txID ids.ID, options ...rpc.Option) (choices.Status, error) {
	res := &GetTxStatusReply{}
	err := c.requester.SendRequest(ctx, "avm.getTxStatus", &api.JSONTxID{
		TxID: txID,
	}, res, options...)
	return res.Status, err
}

func (c *client) GetTx(ctx context.Context, txID ids.ID, options ...rpc.Option) ([]byte, error) {
	res := &api.FormattedTx{}
	err := c.requester.SendRequest(ctx, "avm.getTx", &api.GetTxArgs{
		TxID:     txID,
		Encoding: formatting.Hex,
	}, res, options...)
	if err != nil {
		return nil, err
	}
	return formatting.Decode(res.Encoding, res.Tx)
}

func (c *client) GetUTXOs(
	ctx context.Context,
	addrs []ids.ShortID,
	limit uint32,
	startAddress ids.ShortID,
	startUTXOID ids.ID,
	options ...rpc.Option,
) ([][]byte, ids.ShortID, ids.ID, error) {
	return c.GetAtomicUTXOs(ctx, addrs, "", limit, startAddress, startUTXOID, options...)
}

func (c *client) GetAtomicUTXOs(
	ctx context.Context,
	addrs []ids.ShortID,
	sourceChain string,
	limit uint32,
	startAddress ids.ShortID,
	startUTXOID ids.ID,
	options ...rpc.Option,
) ([][]byte, ids.ShortID, ids.ID, error) {
	res := &api.GetUTXOsReply{}
	err := c.requester.SendRequest(ctx, "avm.getUTXOs", &api.GetUTXOsArgs{
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

func (c *client) GetAssetDescription(ctx context.Context, assetID string, options ...rpc.Option) (*GetAssetDescriptionReply, error) {
	res := &GetAssetDescriptionReply{}
	err := c.requester.SendRequest(ctx, "avm.getAssetDescription", &GetAssetDescriptionArgs{
		AssetID: assetID,
	}, res, options...)
	return res, err
}

func (c *client) GetBalance(
	ctx context.Context,
	addr ids.ShortID,
	assetID string,
	includePartial bool,
	options ...rpc.Option,
) (*GetBalanceReply, error) {
	res := &GetBalanceReply{}
	err := c.requester.SendRequest(ctx, "avm.getBalance", &GetBalanceArgs{
		Address:        addr.String(),
		AssetID:        assetID,
		IncludePartial: includePartial,
	}, res, options...)
	return res, err
}

func (c *client) GetAllBalances(
	ctx context.Context,
	addr ids.ShortID,
	includePartial bool,
	options ...rpc.Option,
) ([]Balance, error) {
	res := &GetAllBalancesReply{}
	err := c.requester.SendRequest(ctx, "avm.getAllBalances", &GetAllBalancesArgs{
		JSONAddress:    api.JSONAddress{Address: addr.String()},
		IncludePartial: includePartial,
	}, res, options...)
	return res.Balances, err
}

// ClientHolder describes how much an address owns of an asset
type ClientHolder struct {
	Amount  uint64
	Address ids.ShortID
}

// ClientOwners describes who can perform an action
type ClientOwners struct {
	Threshold uint32
	Minters   []ids.ShortID
}

func (c *client) CreateAsset(
	ctx context.Context,
	user api.UserPass,
	from []ids.ShortID,
	changeAddr ids.ShortID,
	name string,
	symbol string,
	denomination byte,
	clientHolders []*ClientHolder,
	clientMinters []ClientOwners,
	options ...rpc.Option,
) (ids.ID, error) {
	res := &FormattedAssetID{}
	holders := make([]*Holder, len(clientHolders))
	for i, clientHolder := range clientHolders {
		holders[i] = &Holder{
			Amount:  json.Uint64(clientHolder.Amount),
			Address: clientHolder.Address.String(),
		}
	}
	minters := make([]Owners, len(clientMinters))
	for i, clientMinter := range clientMinters {
		minters[i] = Owners{
			Threshold: json.Uint32(clientMinter.Threshold),
			Minters:   ids.ShortIDsToStrings(clientMinter.Minters),
		}
	}
	err := c.requester.SendRequest(ctx, "avm.createAsset", &CreateAssetArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: ids.ShortIDsToStrings(from)},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr.String()},
		},
		Name:           name,
		Symbol:         symbol,
		Denomination:   denomination,
		InitialHolders: holders,
		MinterSets:     minters,
	}, res, options...)
	return res.AssetID, err
}

func (c *client) CreateFixedCapAsset(
	ctx context.Context,
	user api.UserPass,
	from []ids.ShortID,
	changeAddr ids.ShortID,
	name string,
	symbol string,
	denomination byte,
	clientHolders []*ClientHolder,
	options ...rpc.Option,
) (ids.ID, error) {
	res := &FormattedAssetID{}
	holders := make([]*Holder, len(clientHolders))
	for i, clientHolder := range clientHolders {
		holders[i] = &Holder{
			Amount:  json.Uint64(clientHolder.Amount),
			Address: clientHolder.Address.String(),
		}
	}
	err := c.requester.SendRequest(ctx, "avm.createAsset", &CreateAssetArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: ids.ShortIDsToStrings(from)},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr.String()},
		},
		Name:           name,
		Symbol:         symbol,
		Denomination:   denomination,
		InitialHolders: holders,
	}, res, options...)
	return res.AssetID, err
}

func (c *client) CreateVariableCapAsset(
	ctx context.Context,
	user api.UserPass,
	from []ids.ShortID,
	changeAddr ids.ShortID,
	name string,
	symbol string,
	denomination byte,
	clientMinters []ClientOwners,
	options ...rpc.Option,
) (ids.ID, error) {
	res := &FormattedAssetID{}
	minters := make([]Owners, len(clientMinters))
	for i, clientMinter := range clientMinters {
		minters[i] = Owners{
			Threshold: json.Uint32(clientMinter.Threshold),
			Minters:   ids.ShortIDsToStrings(clientMinter.Minters),
		}
	}
	err := c.requester.SendRequest(ctx, "avm.createAsset", &CreateAssetArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: ids.ShortIDsToStrings(from)},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr.String()},
		},
		Name:         name,
		Symbol:       symbol,
		Denomination: denomination,
		MinterSets:   minters,
	}, res, options...)
	return res.AssetID, err
}

func (c *client) CreateNFTAsset(
	ctx context.Context,
	user api.UserPass,
	from []ids.ShortID,
	changeAddr ids.ShortID,
	name string,
	symbol string,
	clientMinters []ClientOwners,
	options ...rpc.Option,
) (ids.ID, error) {
	res := &FormattedAssetID{}
	minters := make([]Owners, len(clientMinters))
	for i, clientMinter := range clientMinters {
		minters[i] = Owners{
			Threshold: json.Uint32(clientMinter.Threshold),
			Minters:   ids.ShortIDsToStrings(clientMinter.Minters),
		}
	}
	err := c.requester.SendRequest(ctx, "avm.createNFTAsset", &CreateNFTAssetArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: ids.ShortIDsToStrings(from)},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr.String()},
		},
		Name:       name,
		Symbol:     symbol,
		MinterSets: minters,
	}, res, options...)
	return res.AssetID, err
}

func (c *client) CreateAddress(ctx context.Context, user api.UserPass, options ...rpc.Option) (ids.ShortID, error) {
	res := &api.JSONAddress{}
	err := c.requester.SendRequest(ctx, "avm.createAddress", &user, res, options...)
	if err != nil {
		return ids.ShortID{}, err
	}
	return address.ParseToID(res.Address)
}

func (c *client) ListAddresses(ctx context.Context, user api.UserPass, options ...rpc.Option) ([]ids.ShortID, error) {
	res := &api.JSONAddresses{}
	err := c.requester.SendRequest(ctx, "avm.listAddresses", &user, res, options...)
	if err != nil {
		return nil, err
	}
	return address.ParseToIDs(res.Addresses)
}

func (c *client) ExportKey(ctx context.Context, user api.UserPass, addr ids.ShortID, options ...rpc.Option) (*secp256k1.PrivateKey, error) {
	res := &ExportKeyReply{}
	err := c.requester.SendRequest(ctx, "avm.exportKey", &ExportKeyArgs{
		UserPass: user,
		Address:  addr.String(),
	}, res, options...)
	return res.PrivateKey, err
}

func (c *client) ImportKey(ctx context.Context, user api.UserPass, privateKey *secp256k1.PrivateKey, options ...rpc.Option) (ids.ShortID, error) {
	res := &api.JSONAddress{}
	err := c.requester.SendRequest(ctx, "avm.importKey", &ImportKeyArgs{
		UserPass:   user,
		PrivateKey: privateKey,
	}, res, options...)
	if err != nil {
		return ids.ShortID{}, err
	}
	return address.ParseToID(res.Address)
}

func (c *client) Send(
	ctx context.Context,
	user api.UserPass,
	from []ids.ShortID,
	changeAddr ids.ShortID,
	amount uint64,
	assetID string,
	to ids.ShortID,
	memo string,
	options ...rpc.Option,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest(ctx, "avm.send", &SendArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: ids.ShortIDsToStrings(from)},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr.String()},
		},
		SendOutput: SendOutput{
			Amount:  json.Uint64(amount),
			AssetID: assetID,
			To:      to.String(),
		},
		Memo: memo,
	}, res, options...)
	return res.TxID, err
}

func (c *client) SendMultiple(
	ctx context.Context,
	user api.UserPass,
	from []ids.ShortID,
	changeAddr ids.ShortID,
	clientOutputs []ClientSendOutput,
	memo string,
	options ...rpc.Option,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	outputs := make([]SendOutput, len(clientOutputs))
	for i, clientOutput := range clientOutputs {
		outputs[i] = SendOutput{
			Amount:  json.Uint64(clientOutput.Amount),
			AssetID: clientOutput.AssetID,
			To:      clientOutput.To.String(),
		}
	}
	err := c.requester.SendRequest(ctx, "avm.sendMultiple", &SendMultipleArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: ids.ShortIDsToStrings(from)},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr.String()},
		},
		Outputs: outputs,
		Memo:    memo,
	}, res, options...)
	return res.TxID, err
}

func (c *client) Mint(
	ctx context.Context,
	user api.UserPass,
	from []ids.ShortID,
	changeAddr ids.ShortID,
	amount uint64,
	assetID string,
	to ids.ShortID,
	options ...rpc.Option,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest(ctx, "avm.mint", &MintArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: ids.ShortIDsToStrings(from)},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr.String()},
		},
		Amount:  json.Uint64(amount),
		AssetID: assetID,
		To:      to.String(),
	}, res, options...)
	return res.TxID, err
}

func (c *client) SendNFT(
	ctx context.Context,
	user api.UserPass,
	from []ids.ShortID,
	changeAddr ids.ShortID,
	assetID string,
	groupID uint32,
	to ids.ShortID,
	options ...rpc.Option,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest(ctx, "avm.sendNFT", &SendNFTArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: ids.ShortIDsToStrings(from)},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr.String()},
		},
		AssetID: assetID,
		GroupID: json.Uint32(groupID),
		To:      to.String(),
	}, res, options...)
	return res.TxID, err
}

func (c *client) MintNFT(
	ctx context.Context,
	user api.UserPass,
	from []ids.ShortID,
	changeAddr ids.ShortID,
	assetID string,
	payload []byte,
	to ids.ShortID,
	options ...rpc.Option,
) (ids.ID, error) {
	payloadStr, err := formatting.Encode(formatting.Hex, payload)
	if err != nil {
		return ids.Empty, err
	}
	res := &api.JSONTxID{}
	err = c.requester.SendRequest(ctx, "avm.mintNFT", &MintNFTArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: ids.ShortIDsToStrings(from)},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr.String()},
		},
		AssetID:  assetID,
		Payload:  payloadStr,
		To:       to.String(),
		Encoding: formatting.Hex,
	}, res, options...)
	return res.TxID, err
}

func (c *client) Import(ctx context.Context, user api.UserPass, to ids.ShortID, sourceChain string, options ...rpc.Option) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest(ctx, "avm.import", &ImportArgs{
		UserPass:    user,
		To:          to.String(),
		SourceChain: sourceChain,
	}, res, options...)
	return res.TxID, err
}

func (c *client) Export(
	ctx context.Context,
	user api.UserPass,
	from []ids.ShortID,
	changeAddr ids.ShortID,
	amount uint64,
	to ids.ShortID,
	targetChain string,
	assetID string,
	options ...rpc.Option,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest(ctx, "avm.export", &ExportArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: ids.ShortIDsToStrings(from)},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr.String()},
		},
		Amount:      json.Uint64(amount),
		TargetChain: targetChain,
		To:          to.String(),
		AssetID:     assetID,
	}, res, options...)
	return res.TxID, err
}

func AwaitTxAccepted(
	c Client,
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
