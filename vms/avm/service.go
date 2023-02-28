// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"errors"
	"fmt"
	"math"
	"net/http"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/keystore"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	// Max number of addresses that can be passed in as argument to GetUTXOs
	maxGetUTXOsAddrs = 1024

	// Max number of items allowed in a page
	maxPageSize uint64 = 1024
)

var (
	errUnknownAssetID     = errors.New("unknown asset ID")
	errTxNotCreateAsset   = errors.New("transaction doesn't create an asset")
	errNoMinters          = errors.New("no minters provided")
	errNoHoldersOrMinters = errors.New("no minters or initialHolders provided")
	errZeroAmount         = errors.New("amount must be positive")
	errNoOutputs          = errors.New("no outputs to send")
	errInvalidMintAmount  = errors.New("amount minted must be positive")
	errInvalidUTXO        = errors.New("invalid utxo")
	errNilTxID            = errors.New("nil transaction ID")
	errNoAddresses        = errors.New("no addresses provided")
	errNoKeys             = errors.New("from addresses have no keys or funds")
	errMissingPrivateKey  = errors.New("argument 'privateKey' not given")
)

// Service defines the base service for the asset vm
type Service struct{ vm *VM }

// FormattedAssetID defines a JSON formatted struct containing an assetID as a string
type FormattedAssetID struct {
	AssetID ids.ID `json:"assetID"`
}

// IssueTx attempts to issue a transaction into consensus
func (s *Service) IssueTx(_ *http.Request, args *api.FormattedTx, reply *api.JSONTxID) error {
	s.vm.ctx.Log.Debug("AVM: IssueTx called",
		logging.UserString("tx", args.Tx),
	)

	txBytes, err := formatting.Decode(args.Encoding, args.Tx)
	if err != nil {
		return fmt.Errorf("problem decoding transaction: %w", err)
	}
	txID, err := s.vm.IssueTx(txBytes)
	if err != nil {
		return err
	}

	reply.TxID = txID
	return nil
}

func (s *Service) IssueStopVertex(_ *http.Request, _, _ *struct{}) error {
	return s.vm.issueStopVertex()
}

// GetTxStatusReply defines the GetTxStatus replies returned from the API
type GetTxStatusReply struct {
	Status choices.Status `json:"status"`
}

type GetAddressTxsArgs struct {
	api.JSONAddress
	// Cursor used as a page index / offset
	Cursor json.Uint64 `json:"cursor"`
	// PageSize num of items per page
	PageSize json.Uint64 `json:"pageSize"`
	// AssetID defaulted to AVAX if omitted or left blank
	AssetID string `json:"assetID"`
}

type GetAddressTxsReply struct {
	TxIDs []ids.ID `json:"txIDs"`
	// Cursor used as a page index / offset
	Cursor json.Uint64 `json:"cursor"`
}

// GetAddressTxs returns list of transactions for a given address
func (s *Service) GetAddressTxs(_ *http.Request, args *GetAddressTxsArgs, reply *GetAddressTxsReply) error {
	cursor := uint64(args.Cursor)
	pageSize := uint64(args.PageSize)
	s.vm.ctx.Log.Debug("AVM: GetAddressTxs called",
		logging.UserString("address", args.Address),
		logging.UserString("assetID", args.AssetID),
		zap.Uint64("cursor", cursor),
		zap.Uint64("pageSize", pageSize),
	)
	if pageSize > maxPageSize {
		return fmt.Errorf("pageSize > maximum allowed (%d)", maxPageSize)
	} else if pageSize == 0 {
		pageSize = maxPageSize
	}

	// Parse to address
	address, err := avax.ParseServiceAddress(s.vm, args.Address)
	if err != nil {
		return fmt.Errorf("couldn't parse argument 'address' to address: %w", err)
	}

	// Lookup assetID
	assetID, err := s.vm.lookupAssetID(args.AssetID)
	if err != nil {
		return fmt.Errorf("specified `assetID` is invalid: %w", err)
	}

	s.vm.ctx.Log.Debug("fetching transactions",
		logging.UserString("address", args.Address),
		logging.UserString("assetID", args.AssetID),
		zap.Uint64("cursor", cursor),
		zap.Uint64("pageSize", pageSize),
	)

	// Read transactions from the indexer
	reply.TxIDs, err = s.vm.addressTxsIndexer.Read(address[:], assetID, cursor, pageSize)
	if err != nil {
		return err
	}
	s.vm.ctx.Log.Debug("fetched transactions",
		logging.UserString("address", args.Address),
		logging.UserString("assetID", args.AssetID),
		zap.Int("numTxs", len(reply.TxIDs)),
	)

	// To get the next set of tx IDs, the user should provide this cursor.
	// e.g. if they provided cursor 5, and read 6 tx IDs, they should start
	// next time from index (cursor) 11.
	reply.Cursor = json.Uint64(cursor + uint64(len(reply.TxIDs)))
	return nil
}

// GetTxStatus returns the status of the specified transaction
func (s *Service) GetTxStatus(_ *http.Request, args *api.JSONTxID, reply *GetTxStatusReply) error {
	s.vm.ctx.Log.Debug("AVM: GetTxStatus called",
		zap.Stringer("txID", args.TxID),
	)

	if args.TxID == ids.Empty {
		return errNilTxID
	}

	tx := UniqueTx{
		vm:   s.vm,
		txID: args.TxID,
	}

	reply.Status = tx.Status()
	return nil
}

// GetTx returns the specified transaction
func (s *Service) GetTx(_ *http.Request, args *api.GetTxArgs, reply *api.GetTxReply) error {
	s.vm.ctx.Log.Debug("AVM: GetTx called",
		zap.Stringer("txID", args.TxID),
	)

	if args.TxID == ids.Empty {
		return errNilTxID
	}

	tx := UniqueTx{
		vm:   s.vm,
		txID: args.TxID,
	}
	if status := tx.Status(); !status.Fetched() {
		return errUnknownTx
	}

	reply.Encoding = args.Encoding

	if args.Encoding == formatting.JSON {
		reply.Tx = tx
		return tx.Unsigned.Visit(&txInit{
			tx:            tx.Tx,
			ctx:           s.vm.ctx,
			typeToFxIndex: s.vm.typeToFxIndex,
			fxs:           s.vm.fxs,
		})
	}

	var err error
	reply.Tx, err = formatting.Encode(args.Encoding, tx.Bytes())
	if err != nil {
		return fmt.Errorf("couldn't encode tx as string: %w", err)
	}
	return nil
}

// GetUTXOs gets all utxos for passed in addresses
func (s *Service) GetUTXOs(_ *http.Request, args *api.GetUTXOsArgs, reply *api.GetUTXOsReply) error {
	s.vm.ctx.Log.Debug("AVM: GetUTXOs called",
		logging.UserStrings("addresses", args.Addresses),
	)

	if len(args.Addresses) == 0 {
		return errNoAddresses
	}
	if len(args.Addresses) > maxGetUTXOsAddrs {
		return fmt.Errorf("number of addresses given, %d, exceeds maximum, %d", len(args.Addresses), maxGetUTXOsAddrs)
	}

	var sourceChain ids.ID
	if args.SourceChain == "" {
		sourceChain = s.vm.ctx.ChainID
	} else {
		chainID, err := s.vm.ctx.BCLookup.Lookup(args.SourceChain)
		if err != nil {
			return fmt.Errorf("problem parsing source chainID %q: %w", args.SourceChain, err)
		}
		sourceChain = chainID
	}

	addrSet, err := avax.ParseServiceAddresses(s.vm, args.Addresses)
	if err != nil {
		return err
	}

	startAddr := ids.ShortEmpty
	startUTXO := ids.Empty
	if args.StartIndex.Address != "" || args.StartIndex.UTXO != "" {
		startAddr, err = avax.ParseServiceAddress(s.vm, args.StartIndex.Address)
		if err != nil {
			return fmt.Errorf("couldn't parse start index address %q: %w", args.StartIndex.Address, err)
		}
		startUTXO, err = ids.FromString(args.StartIndex.UTXO)
		if err != nil {
			return fmt.Errorf("couldn't parse start index utxo: %w", err)
		}
	}

	var (
		utxos     []*avax.UTXO
		endAddr   ids.ShortID
		endUTXOID ids.ID
	)
	limit := int(args.Limit)
	if limit <= 0 || int(maxPageSize) < limit {
		limit = int(maxPageSize)
	}
	if sourceChain == s.vm.ctx.ChainID {
		utxos, endAddr, endUTXOID, err = avax.GetPaginatedUTXOs(
			s.vm.state,
			addrSet,
			startAddr,
			startUTXO,
			limit,
		)
	} else {
		utxos, endAddr, endUTXOID, err = s.vm.GetAtomicUTXOs(
			sourceChain,
			addrSet,
			startAddr,
			startUTXO,
			limit,
		)
	}
	if err != nil {
		return fmt.Errorf("problem retrieving UTXOs: %w", err)
	}

	reply.UTXOs = make([]string, len(utxos))
	codec := s.vm.parser.Codec()
	for i, utxo := range utxos {
		b, err := codec.Marshal(txs.CodecVersion, utxo)
		if err != nil {
			return fmt.Errorf("problem marshalling UTXO: %w", err)
		}
		reply.UTXOs[i], err = formatting.Encode(args.Encoding, b)
		if err != nil {
			return fmt.Errorf("couldn't encode UTXO %s as string: %w", utxo.InputID(), err)
		}
	}

	endAddress, err := s.vm.FormatLocalAddress(endAddr)
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}

	reply.EndIndex.Address = endAddress
	reply.EndIndex.UTXO = endUTXOID.String()
	reply.NumFetched = json.Uint64(len(utxos))
	reply.Encoding = args.Encoding
	return nil
}

// GetAssetDescriptionArgs are arguments for passing into GetAssetDescription requests
type GetAssetDescriptionArgs struct {
	AssetID string `json:"assetID"`
}

// GetAssetDescriptionReply defines the GetAssetDescription replies returned from the API
type GetAssetDescriptionReply struct {
	FormattedAssetID
	Name         string     `json:"name"`
	Symbol       string     `json:"symbol"`
	Denomination json.Uint8 `json:"denomination"`
}

// GetAssetDescription creates an empty account with the name passed in
func (s *Service) GetAssetDescription(_ *http.Request, args *GetAssetDescriptionArgs, reply *GetAssetDescriptionReply) error {
	s.vm.ctx.Log.Debug("AVM: GetAssetDescription called",
		logging.UserString("assetID", args.AssetID),
	)

	assetID, err := s.vm.lookupAssetID(args.AssetID)
	if err != nil {
		return err
	}

	tx := &UniqueTx{
		vm:   s.vm,
		txID: assetID,
	}
	if status := tx.Status(); !status.Fetched() {
		return errUnknownAssetID
	}
	createAssetTx, ok := tx.Unsigned.(*txs.CreateAssetTx)
	if !ok {
		return errTxNotCreateAsset
	}

	reply.AssetID = assetID
	reply.Name = createAssetTx.Name
	reply.Symbol = createAssetTx.Symbol
	reply.Denomination = json.Uint8(createAssetTx.Denomination)

	return nil
}

// GetBalanceArgs are arguments for passing into GetBalance requests
type GetBalanceArgs struct {
	Address        string `json:"address"`
	AssetID        string `json:"assetID"`
	IncludePartial bool   `json:"includePartial"`
}

// GetBalanceReply defines the GetBalance replies returned from the API
type GetBalanceReply struct {
	Balance json.Uint64   `json:"balance"`
	UTXOIDs []avax.UTXOID `json:"utxoIDs"`
}

// GetBalance returns the balance of an asset held by an address.
// If ![args.IncludePartial], returns only the balance held solely
// (1 out of 1 multisig) by the address and with a locktime in the past.
// Otherwise, returned balance includes assets held only partially by the
// address, and includes balances with locktime in the future.
func (s *Service) GetBalance(_ *http.Request, args *GetBalanceArgs, reply *GetBalanceReply) error {
	s.vm.ctx.Log.Debug("AVM: GetBalance called",
		logging.UserString("address", args.Address),
		logging.UserString("assetID", args.AssetID),
	)

	addr, err := avax.ParseServiceAddress(s.vm, args.Address)
	if err != nil {
		return fmt.Errorf("problem parsing address '%s': %w", args.Address, err)
	}

	assetID, err := s.vm.lookupAssetID(args.AssetID)
	if err != nil {
		return err
	}

	addrSet := set.Set[ids.ShortID]{}
	addrSet.Add(addr)

	utxos, err := avax.GetAllUTXOs(s.vm.state, addrSet)
	if err != nil {
		return fmt.Errorf("problem retrieving UTXOs: %w", err)
	}

	now := s.vm.clock.Unix()
	reply.UTXOIDs = make([]avax.UTXOID, 0, len(utxos))
	for _, utxo := range utxos {
		if utxo.AssetID() != assetID {
			continue
		}
		// TODO make this not specific to *secp256k1fx.TransferOutput
		transferable, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			continue
		}
		owners := transferable.OutputOwners
		if !args.IncludePartial && (len(owners.Addrs) != 1 || owners.Locktime > now) {
			continue
		}
		amt, err := safemath.Add64(transferable.Amount(), uint64(reply.Balance))
		if err != nil {
			return err
		}
		reply.Balance = json.Uint64(amt)
		reply.UTXOIDs = append(reply.UTXOIDs, utxo.UTXOID)
	}

	return nil
}

type Balance struct {
	AssetID string      `json:"asset"`
	Balance json.Uint64 `json:"balance"`
}

type GetAllBalancesArgs struct {
	api.JSONAddress
	IncludePartial bool `json:"includePartial"`
}

// GetAllBalancesReply is the response from a call to GetAllBalances
type GetAllBalancesReply struct {
	Balances []Balance `json:"balances"`
}

// GetAllBalances returns a map where:
//
// Key: ID of an asset such that [args.Address] has a non-zero balance of the asset
// Value: The balance of the asset held by the address
//
// If ![args.IncludePartial], returns only unlocked balance/UTXOs with a 1-out-of-1 multisig.
// Otherwise, returned balance/UTXOs includes assets held only partially by the
// address, and includes balances with locktime in the future.
func (s *Service) GetAllBalances(_ *http.Request, args *GetAllBalancesArgs, reply *GetAllBalancesReply) error {
	s.vm.ctx.Log.Debug("AVM: GetAllBalances called",
		logging.UserString("address", args.Address),
	)

	address, err := avax.ParseServiceAddress(s.vm, args.Address)
	if err != nil {
		return fmt.Errorf("problem parsing address '%s': %w", args.Address, err)
	}
	addrSet := set.Set[ids.ShortID]{}
	addrSet.Add(address)

	utxos, err := avax.GetAllUTXOs(s.vm.state, addrSet)
	if err != nil {
		return fmt.Errorf("couldn't get address's UTXOs: %w", err)
	}

	now := s.vm.clock.Unix()
	assetIDs := set.Set[ids.ID]{}       // IDs of assets the address has a non-zero balance of
	balances := make(map[ids.ID]uint64) // key: ID (as bytes). value: balance of that asset
	for _, utxo := range utxos {
		// TODO make this not specific to *secp256k1fx.TransferOutput
		transferable, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			continue
		}
		owners := transferable.OutputOwners
		if !args.IncludePartial && (len(owners.Addrs) != 1 || owners.Locktime > now) {
			continue
		}
		assetID := utxo.AssetID()
		assetIDs.Add(assetID)
		balance := balances[assetID] // 0 if key doesn't exist
		balance, err := safemath.Add64(transferable.Amount(), balance)
		if err != nil {
			balances[assetID] = math.MaxUint64
		} else {
			balances[assetID] = balance
		}
	}

	reply.Balances = make([]Balance, assetIDs.Len())
	i := 0
	for assetID := range assetIDs {
		alias := s.vm.PrimaryAliasOrDefault(assetID)
		reply.Balances[i] = Balance{
			AssetID: alias,
			Balance: json.Uint64(balances[assetID]),
		}
		i++
	}

	return nil
}

// Holder describes how much an address owns of an asset
type Holder struct {
	Amount  json.Uint64 `json:"amount"`
	Address string      `json:"address"`
}

// Owners describes who can perform an action
type Owners struct {
	Threshold json.Uint32 `json:"threshold"`
	Minters   []string    `json:"minters"`
}

// CreateAssetArgs are arguments for passing into CreateAsset
type CreateAssetArgs struct {
	api.JSONSpendHeader           // User, password, from addrs, change addr
	Name                string    `json:"name"`
	Symbol              string    `json:"symbol"`
	Denomination        byte      `json:"denomination"`
	InitialHolders      []*Holder `json:"initialHolders"`
	MinterSets          []Owners  `json:"minterSets"`
}

// AssetIDChangeAddr is an asset ID and a change address
type AssetIDChangeAddr struct {
	FormattedAssetID
	api.JSONChangeAddr
}

// CreateAsset returns ID of the newly created asset
func (s *Service) CreateAsset(_ *http.Request, args *CreateAssetArgs, reply *AssetIDChangeAddr) error {
	s.vm.ctx.Log.Debug("AVM: CreateAsset called",
		logging.UserString("name", args.Name),
		logging.UserString("symbol", args.Symbol),
		zap.Int("numInitialHolders", len(args.InitialHolders)),
		zap.Int("numMinters", len(args.MinterSets)),
	)

	if len(args.InitialHolders) == 0 && len(args.MinterSets) == 0 {
		return errNoHoldersOrMinters
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.vm, args.From)
	if err != nil {
		return err
	}

	// Get the UTXOs/keys for the from addresses
	utxos, kc, err := s.vm.LoadUser(args.Username, args.Password, fromAddrs)
	if err != nil {
		return err
	}

	// Parse the change address.
	if len(kc.Keys) == 0 {
		return errNoKeys
	}
	changeAddr, err := s.vm.selectChangeAddr(kc.Keys[0].PublicKey().Address(), args.ChangeAddr)
	if err != nil {
		return err
	}

	amountsSpent, ins, keys, err := s.vm.Spend(
		utxos,
		kc,
		map[ids.ID]uint64{
			s.vm.feeAssetID: s.vm.CreateAssetTxFee,
		},
	)
	if err != nil {
		return err
	}

	outs := []*avax.TransferableOutput{}
	if amountSpent := amountsSpent[s.vm.feeAssetID]; amountSpent > s.vm.CreateAssetTxFee {
		outs = append(outs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: s.vm.feeAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amountSpent - s.vm.CreateAssetTxFee,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{changeAddr},
				},
			},
		})
	}

	initialState := &txs.InitialState{
		FxIndex: 0, // TODO: Should lookup secp256k1fx FxID
		Outs:    make([]verify.State, 0, len(args.InitialHolders)+len(args.MinterSets)),
	}
	for _, holder := range args.InitialHolders {
		addr, err := avax.ParseServiceAddress(s.vm, holder.Address)
		if err != nil {
			return err
		}
		initialState.Outs = append(initialState.Outs, &secp256k1fx.TransferOutput{
			Amt: uint64(holder.Amount),
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr},
			},
		})
	}
	for _, owner := range args.MinterSets {
		minter := &secp256k1fx.MintOutput{
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: uint32(owner.Threshold),
				Addrs:     make([]ids.ShortID, 0, len(owner.Minters)),
			},
		}
		minterAddrsSet, err := avax.ParseServiceAddresses(s.vm, owner.Minters)
		if err != nil {
			return err
		}
		minter.Addrs = minterAddrsSet.List()
		utils.Sort(minter.Addrs)
		initialState.Outs = append(initialState.Outs, minter)
	}
	initialState.Sort(s.vm.parser.Codec())

	tx := txs.Tx{Unsigned: &txs.CreateAssetTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    s.vm.ctx.NetworkID,
			BlockchainID: s.vm.ctx.ChainID,
			Outs:         outs,
			Ins:          ins,
		}},
		Name:         args.Name,
		Symbol:       args.Symbol,
		Denomination: args.Denomination,
		States:       []*txs.InitialState{initialState},
	}}
	if err := tx.SignSECP256K1Fx(s.vm.parser.Codec(), keys); err != nil {
		return err
	}

	assetID, err := s.vm.IssueTx(tx.Bytes())
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.AssetID = assetID
	reply.ChangeAddr, err = s.vm.FormatLocalAddress(changeAddr)
	return err
}

// CreateFixedCapAsset returns ID of the newly created asset
func (s *Service) CreateFixedCapAsset(_ *http.Request, args *CreateAssetArgs, reply *AssetIDChangeAddr) error {
	s.vm.ctx.Log.Debug("AVM: CreateFixedCapAsset called",
		logging.UserString("name", args.Name),
		logging.UserString("symbol", args.Symbol),
		zap.Int("numInitialHolders", len(args.InitialHolders)),
	)

	return s.CreateAsset(nil, args, reply)
}

// CreateVariableCapAsset returns ID of the newly created asset
func (s *Service) CreateVariableCapAsset(_ *http.Request, args *CreateAssetArgs, reply *AssetIDChangeAddr) error {
	s.vm.ctx.Log.Debug("AVM: CreateVariableCapAsset called",
		logging.UserString("name", args.Name),
		logging.UserString("symbol", args.Symbol),
		zap.Int("numMinters", len(args.MinterSets)),
	)

	return s.CreateAsset(nil, args, reply)
}

// CreateNFTAssetArgs are arguments for passing into CreateNFTAsset requests
type CreateNFTAssetArgs struct {
	api.JSONSpendHeader          // User, password, from addrs, change addr
	Name                string   `json:"name"`
	Symbol              string   `json:"symbol"`
	MinterSets          []Owners `json:"minterSets"`
}

// CreateNFTAsset returns ID of the newly created asset
func (s *Service) CreateNFTAsset(_ *http.Request, args *CreateNFTAssetArgs, reply *AssetIDChangeAddr) error {
	s.vm.ctx.Log.Debug("AVM: CreateNFTAsset called",
		logging.UserString("name", args.Name),
		logging.UserString("symbol", args.Symbol),
		zap.Int("numMinters", len(args.MinterSets)),
	)

	if len(args.MinterSets) == 0 {
		return errNoMinters
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.vm, args.From)
	if err != nil {
		return err
	}

	// Get the UTXOs/keys for the from addresses
	utxos, kc, err := s.vm.LoadUser(args.Username, args.Password, fromAddrs)
	if err != nil {
		return err
	}

	// Parse the change address.
	if len(kc.Keys) == 0 {
		return errNoKeys
	}
	changeAddr, err := s.vm.selectChangeAddr(kc.Keys[0].PublicKey().Address(), args.ChangeAddr)
	if err != nil {
		return err
	}

	amountsSpent, ins, keys, err := s.vm.Spend(
		utxos,
		kc,
		map[ids.ID]uint64{
			s.vm.feeAssetID: s.vm.CreateAssetTxFee,
		},
	)
	if err != nil {
		return err
	}

	outs := []*avax.TransferableOutput{}
	if amountSpent := amountsSpent[s.vm.feeAssetID]; amountSpent > s.vm.CreateAssetTxFee {
		outs = append(outs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: s.vm.feeAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amountSpent - s.vm.CreateAssetTxFee,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{changeAddr},
				},
			},
		})
	}

	initialState := &txs.InitialState{
		FxIndex: 1, // TODO: Should lookup nftfx FxID
		Outs:    make([]verify.State, 0, len(args.MinterSets)),
	}
	for i, owner := range args.MinterSets {
		minter := &nftfx.MintOutput{
			GroupID: uint32(i),
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: uint32(owner.Threshold),
			},
		}
		minterAddrsSet, err := avax.ParseServiceAddresses(s.vm, owner.Minters)
		if err != nil {
			return err
		}
		minter.Addrs = minterAddrsSet.List()
		utils.Sort(minter.Addrs)
		initialState.Outs = append(initialState.Outs, minter)
	}
	initialState.Sort(s.vm.parser.Codec())

	tx := txs.Tx{Unsigned: &txs.CreateAssetTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    s.vm.ctx.NetworkID,
			BlockchainID: s.vm.ctx.ChainID,
			Outs:         outs,
			Ins:          ins,
		}},
		Name:         args.Name,
		Symbol:       args.Symbol,
		Denomination: 0, // NFTs are non-fungible
		States:       []*txs.InitialState{initialState},
	}}
	if err := tx.SignSECP256K1Fx(s.vm.parser.Codec(), keys); err != nil {
		return err
	}

	assetID, err := s.vm.IssueTx(tx.Bytes())
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.AssetID = assetID
	reply.ChangeAddr, err = s.vm.FormatLocalAddress(changeAddr)
	return err
}

// CreateAddress creates an address for the user [args.Username]
func (s *Service) CreateAddress(_ *http.Request, args *api.UserPass, reply *api.JSONAddress) error {
	s.vm.ctx.Log.Debug("AVM: CreateAddress called",
		logging.UserString("username", args.Username),
	)

	user, err := keystore.NewUserFromKeystore(s.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return err
	}
	defer user.Close()

	sk, err := keystore.NewKey(user)
	if err != nil {
		return err
	}

	reply.Address, err = s.vm.FormatLocalAddress(sk.PublicKey().Address())
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}

	// Return an error if the DB can't close, this will execute before the above
	// db close.
	return user.Close()
}

// ListAddresses returns all of the addresses controlled by user [args.Username]
func (s *Service) ListAddresses(_ *http.Request, args *api.UserPass, response *api.JSONAddresses) error {
	s.vm.ctx.Log.Debug("AVM: ListAddresses called",
		logging.UserString("username", args.Username),
	)

	user, err := keystore.NewUserFromKeystore(s.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return err
	}

	response.Addresses = []string{}

	addresses, err := user.GetAddresses()
	if err != nil {
		// An error fetching the addresses may just mean that the user has no
		// addresses.
		return user.Close()
	}

	for _, address := range addresses {
		addr, err := s.vm.FormatLocalAddress(address)
		if err != nil {
			// Drop any potential error closing the database to report the
			// original error
			_ = user.Close()
			return fmt.Errorf("problem formatting address: %w", err)
		}
		response.Addresses = append(response.Addresses, addr)
	}
	return user.Close()
}

// ExportKeyArgs are arguments for ExportKey
type ExportKeyArgs struct {
	api.UserPass
	Address string `json:"address"`
}

// ExportKeyReply is the response for ExportKey
type ExportKeyReply struct {
	// The decrypted PrivateKey for the Address provided in the arguments
	PrivateKey *secp256k1.PrivateKey `json:"privateKey"`
}

// ExportKey returns a private key from the provided user
func (s *Service) ExportKey(_ *http.Request, args *ExportKeyArgs, reply *ExportKeyReply) error {
	s.vm.ctx.Log.Debug("AVM: ExportKey called",
		logging.UserString("username", args.Username),
	)

	addr, err := avax.ParseServiceAddress(s.vm, args.Address)
	if err != nil {
		return fmt.Errorf("problem parsing address %q: %w", args.Address, err)
	}

	user, err := keystore.NewUserFromKeystore(s.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return err
	}

	reply.PrivateKey, err = user.GetKey(addr)
	if err != nil {
		// Drop any potential error closing the database to report the original
		// error
		_ = user.Close()
		return fmt.Errorf("problem retrieving private key: %w", err)
	}
	return user.Close()
}

// ImportKeyArgs are arguments for ImportKey
type ImportKeyArgs struct {
	api.UserPass
	PrivateKey *secp256k1.PrivateKey `json:"privateKey"`
}

// ImportKeyReply is the response for ImportKey
type ImportKeyReply struct {
	// The address controlled by the PrivateKey provided in the arguments
	Address string `json:"address"`
}

// ImportKey adds a private key to the provided user
func (s *Service) ImportKey(_ *http.Request, args *ImportKeyArgs, reply *api.JSONAddress) error {
	s.vm.ctx.Log.Debug("AVM: ImportKey called",
		logging.UserString("username", args.Username),
	)

	if args.PrivateKey == nil {
		return errMissingPrivateKey
	}

	user, err := keystore.NewUserFromKeystore(s.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return err
	}
	defer user.Close()

	if err := user.PutKeys(args.PrivateKey); err != nil {
		return fmt.Errorf("problem saving key %w", err)
	}

	newAddress := args.PrivateKey.PublicKey().Address()
	reply.Address, err = s.vm.FormatLocalAddress(newAddress)
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}

	return user.Close()
}

// SendOutput specifies that [Amount] of asset [AssetID] be sent to [To]
type SendOutput struct {
	// The amount of funds to send
	Amount json.Uint64 `json:"amount"`

	// ID of the asset being sent
	AssetID string `json:"assetID"`

	// Address of the recipient
	To string `json:"to"`
}

// SendArgs are arguments for passing into Send requests
type SendArgs struct {
	// User, password, from addrs, change addr
	api.JSONSpendHeader

	// The amount, assetID, and destination to send funds to
	SendOutput

	// Memo field
	Memo string `json:"memo"`
}

// SendMultipleArgs are arguments for passing into SendMultiple requests
type SendMultipleArgs struct {
	// User, password, from addrs, change addr
	api.JSONSpendHeader

	// The outputs of the transaction
	Outputs []SendOutput `json:"outputs"`

	// Memo field
	Memo string `json:"memo"`
}

// Send returns the ID of the newly created transaction
func (s *Service) Send(r *http.Request, args *SendArgs, reply *api.JSONTxIDChangeAddr) error {
	return s.SendMultiple(r, &SendMultipleArgs{
		JSONSpendHeader: args.JSONSpendHeader,
		Outputs:         []SendOutput{args.SendOutput},
		Memo:            args.Memo,
	}, reply)
}

// SendMultiple sends a transaction with multiple outputs.
func (s *Service) SendMultiple(_ *http.Request, args *SendMultipleArgs, reply *api.JSONTxIDChangeAddr) error {
	s.vm.ctx.Log.Debug("AVM: SendMultiple called",
		logging.UserString("username", args.Username),
	)

	// Validate the memo field
	memoBytes := []byte(args.Memo)
	if l := len(memoBytes); l > avax.MaxMemoSize {
		return fmt.Errorf("max memo length is %d but provided memo field is length %d", avax.MaxMemoSize, l)
	} else if len(args.Outputs) == 0 {
		return errNoOutputs
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.vm, args.From)
	if err != nil {
		return err
	}

	// Load user's UTXOs/keys
	utxos, kc, err := s.vm.LoadUser(args.Username, args.Password, fromAddrs)
	if err != nil {
		return err
	}

	// Parse the change address.
	if len(kc.Keys) == 0 {
		return errNoKeys
	}
	changeAddr, err := s.vm.selectChangeAddr(kc.Keys[0].PublicKey().Address(), args.ChangeAddr)
	if err != nil {
		return err
	}

	// Calculate required input amounts and create the desired outputs
	// String repr. of asset ID --> asset ID
	assetIDs := make(map[string]ids.ID)
	// Asset ID --> amount of that asset being sent
	amounts := make(map[ids.ID]uint64)
	// Outputs of our tx
	outs := []*avax.TransferableOutput{}
	for _, output := range args.Outputs {
		if output.Amount == 0 {
			return errZeroAmount
		}
		assetID, ok := assetIDs[output.AssetID] // Asset ID of next output
		if !ok {
			assetID, err = s.vm.lookupAssetID(output.AssetID)
			if err != nil {
				return fmt.Errorf("couldn't find asset %s", output.AssetID)
			}
			assetIDs[output.AssetID] = assetID
		}
		currentAmount := amounts[assetID]
		newAmount, err := safemath.Add64(currentAmount, uint64(output.Amount))
		if err != nil {
			return fmt.Errorf("problem calculating required spend amount: %w", err)
		}
		amounts[assetID] = newAmount

		// Parse the to address
		to, err := avax.ParseServiceAddress(s.vm, output.To)
		if err != nil {
			return fmt.Errorf("problem parsing to address %q: %w", output.To, err)
		}

		// Create the Output
		outs = append(outs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: uint64(output.Amount),
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{to},
				},
			},
		})
	}

	amountsWithFee := make(map[ids.ID]uint64, len(amounts)+1)
	for assetID, amount := range amounts {
		amountsWithFee[assetID] = amount
	}

	amountWithFee, err := safemath.Add64(amounts[s.vm.feeAssetID], s.vm.TxFee)
	if err != nil {
		return fmt.Errorf("problem calculating required spend amount: %w", err)
	}
	amountsWithFee[s.vm.feeAssetID] = amountWithFee

	amountsSpent, ins, keys, err := s.vm.Spend(
		utxos,
		kc,
		amountsWithFee,
	)
	if err != nil {
		return err
	}

	// Add the required change outputs
	for assetID, amountWithFee := range amountsWithFee {
		amountSpent := amountsSpent[assetID]

		if amountSpent > amountWithFee {
			outs = append(outs, &avax.TransferableOutput{
				Asset: avax.Asset{ID: assetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: amountSpent - amountWithFee,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{changeAddr},
					},
				},
			})
		}
	}
	avax.SortTransferableOutputs(outs, s.vm.parser.Codec())

	tx := txs.Tx{Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    s.vm.ctx.NetworkID,
		BlockchainID: s.vm.ctx.ChainID,
		Outs:         outs,
		Ins:          ins,
		Memo:         memoBytes,
	}}}
	if err := tx.SignSECP256K1Fx(s.vm.parser.Codec(), keys); err != nil {
		return err
	}

	txID, err := s.vm.IssueTx(tx.Bytes())
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.TxID = txID
	reply.ChangeAddr, err = s.vm.FormatLocalAddress(changeAddr)
	return err
}

// MintArgs are arguments for passing into Mint requests
type MintArgs struct {
	api.JSONSpendHeader             // User, password, from addrs, change addr
	Amount              json.Uint64 `json:"amount"`
	AssetID             string      `json:"assetID"`
	To                  string      `json:"to"`
}

// Mint issues a transaction that mints more of the asset
func (s *Service) Mint(_ *http.Request, args *MintArgs, reply *api.JSONTxIDChangeAddr) error {
	s.vm.ctx.Log.Debug("AVM: Mint called",
		logging.UserString("username", args.Username),
	)

	if args.Amount == 0 {
		return errInvalidMintAmount
	}

	assetID, err := s.vm.lookupAssetID(args.AssetID)
	if err != nil {
		return err
	}

	to, err := avax.ParseServiceAddress(s.vm, args.To)
	if err != nil {
		return fmt.Errorf("problem parsing to address %q: %w", args.To, err)
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.vm, args.From)
	if err != nil {
		return err
	}

	// Get the UTXOs/keys for the from addresses
	feeUTXOs, feeKc, err := s.vm.LoadUser(args.Username, args.Password, fromAddrs)
	if err != nil {
		return err
	}

	// Parse the change address.
	if len(feeKc.Keys) == 0 {
		return errNoKeys
	}
	changeAddr, err := s.vm.selectChangeAddr(feeKc.Keys[0].PublicKey().Address(), args.ChangeAddr)
	if err != nil {
		return err
	}

	amountsSpent, ins, keys, err := s.vm.Spend(
		feeUTXOs,
		feeKc,
		map[ids.ID]uint64{
			s.vm.feeAssetID: s.vm.TxFee,
		},
	)
	if err != nil {
		return err
	}

	outs := []*avax.TransferableOutput{}
	if amountSpent := amountsSpent[s.vm.feeAssetID]; amountSpent > s.vm.TxFee {
		outs = append(outs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: s.vm.feeAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amountSpent - s.vm.TxFee,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{changeAddr},
				},
			},
		})
	}

	// Get all UTXOs/keys for the user
	utxos, kc, err := s.vm.LoadUser(args.Username, args.Password, nil)
	if err != nil {
		return err
	}

	ops, opKeys, err := s.vm.Mint(
		utxos,
		kc,
		map[ids.ID]uint64{
			assetID: uint64(args.Amount),
		},
		to,
	)
	if err != nil {
		return err
	}
	keys = append(keys, opKeys...)

	tx := txs.Tx{Unsigned: &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    s.vm.ctx.NetworkID,
			BlockchainID: s.vm.ctx.ChainID,
			Outs:         outs,
			Ins:          ins,
		}},
		Ops: ops,
	}}
	if err := tx.SignSECP256K1Fx(s.vm.parser.Codec(), keys); err != nil {
		return err
	}

	txID, err := s.vm.IssueTx(tx.Bytes())
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.TxID = txID
	reply.ChangeAddr, err = s.vm.FormatLocalAddress(changeAddr)
	return err
}

// SendNFTArgs are arguments for passing into SendNFT requests
type SendNFTArgs struct {
	api.JSONSpendHeader             // User, password, from addrs, change addr
	AssetID             string      `json:"assetID"`
	GroupID             json.Uint32 `json:"groupID"`
	To                  string      `json:"to"`
}

// SendNFT sends an NFT
func (s *Service) SendNFT(_ *http.Request, args *SendNFTArgs, reply *api.JSONTxIDChangeAddr) error {
	s.vm.ctx.Log.Debug("AVM: SendNFT called",
		logging.UserString("username", args.Username),
	)

	// Parse the asset ID
	assetID, err := s.vm.lookupAssetID(args.AssetID)
	if err != nil {
		return err
	}

	// Parse the to address
	to, err := avax.ParseServiceAddress(s.vm, args.To)
	if err != nil {
		return fmt.Errorf("problem parsing to address %q: %w", args.To, err)
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.vm, args.From)
	if err != nil {
		return err
	}

	// Get the UTXOs/keys for the from addresses
	utxos, kc, err := s.vm.LoadUser(args.Username, args.Password, fromAddrs)
	if err != nil {
		return err
	}

	// Parse the change address.
	if len(kc.Keys) == 0 {
		return errNoKeys
	}
	changeAddr, err := s.vm.selectChangeAddr(kc.Keys[0].PublicKey().Address(), args.ChangeAddr)
	if err != nil {
		return err
	}

	amountsSpent, ins, secpKeys, err := s.vm.Spend(
		utxos,
		kc,
		map[ids.ID]uint64{
			s.vm.feeAssetID: s.vm.TxFee,
		},
	)
	if err != nil {
		return err
	}

	outs := []*avax.TransferableOutput{}
	if amountSpent := amountsSpent[s.vm.feeAssetID]; amountSpent > s.vm.TxFee {
		outs = append(outs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: s.vm.feeAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amountSpent - s.vm.TxFee,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{changeAddr},
				},
			},
		})
	}

	ops, nftKeys, err := s.vm.SpendNFT(
		utxos,
		kc,
		assetID,
		uint32(args.GroupID),
		to,
	)
	if err != nil {
		return err
	}

	tx := txs.Tx{Unsigned: &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    s.vm.ctx.NetworkID,
			BlockchainID: s.vm.ctx.ChainID,
			Outs:         outs,
			Ins:          ins,
		}},
		Ops: ops,
	}}
	if err := tx.SignSECP256K1Fx(s.vm.parser.Codec(), secpKeys); err != nil {
		return err
	}
	if err := tx.SignNFTFx(s.vm.parser.Codec(), nftKeys); err != nil {
		return err
	}

	txID, err := s.vm.IssueTx(tx.Bytes())
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.TxID = txID
	reply.ChangeAddr, err = s.vm.FormatLocalAddress(changeAddr)
	return err
}

// MintNFTArgs are arguments for passing into MintNFT requests
type MintNFTArgs struct {
	api.JSONSpendHeader                     // User, password, from addrs, change addr
	AssetID             string              `json:"assetID"`
	Payload             string              `json:"payload"`
	To                  string              `json:"to"`
	Encoding            formatting.Encoding `json:"encoding"`
}

// MintNFT issues a MintNFT transaction and returns the ID of the newly created transaction
func (s *Service) MintNFT(_ *http.Request, args *MintNFTArgs, reply *api.JSONTxIDChangeAddr) error {
	s.vm.ctx.Log.Debug("AVM: MintNFT called",
		logging.UserString("username", args.Username),
	)

	assetID, err := s.vm.lookupAssetID(args.AssetID)
	if err != nil {
		return err
	}

	to, err := avax.ParseServiceAddress(s.vm, args.To)
	if err != nil {
		return fmt.Errorf("problem parsing to address %q: %w", args.To, err)
	}

	payloadBytes, err := formatting.Decode(args.Encoding, args.Payload)
	if err != nil {
		return fmt.Errorf("problem decoding payload bytes: %w", err)
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.vm, args.From)
	if err != nil {
		return err
	}

	// Get the UTXOs/keys for the from addresses
	feeUTXOs, feeKc, err := s.vm.LoadUser(args.Username, args.Password, fromAddrs)
	if err != nil {
		return err
	}

	// Parse the change address.
	if len(feeKc.Keys) == 0 {
		return errNoKeys
	}
	changeAddr, err := s.vm.selectChangeAddr(feeKc.Keys[0].PublicKey().Address(), args.ChangeAddr)
	if err != nil {
		return err
	}

	amountsSpent, ins, secpKeys, err := s.vm.Spend(
		feeUTXOs,
		feeKc,
		map[ids.ID]uint64{
			s.vm.feeAssetID: s.vm.TxFee,
		},
	)
	if err != nil {
		return err
	}

	outs := []*avax.TransferableOutput{}
	if amountSpent := amountsSpent[s.vm.feeAssetID]; amountSpent > s.vm.TxFee {
		outs = append(outs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: s.vm.feeAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amountSpent - s.vm.TxFee,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{changeAddr},
				},
			},
		})
	}

	// Get all UTXOs/keys
	utxos, kc, err := s.vm.LoadUser(args.Username, args.Password, nil)
	if err != nil {
		return err
	}

	ops, nftKeys, err := s.vm.MintNFT(
		utxos,
		kc,
		assetID,
		payloadBytes,
		to,
	)
	if err != nil {
		return err
	}

	tx := txs.Tx{Unsigned: &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    s.vm.ctx.NetworkID,
			BlockchainID: s.vm.ctx.ChainID,
			Outs:         outs,
			Ins:          ins,
		}},
		Ops: ops,
	}}
	if err := tx.SignSECP256K1Fx(s.vm.parser.Codec(), secpKeys); err != nil {
		return err
	}
	if err := tx.SignNFTFx(s.vm.parser.Codec(), nftKeys); err != nil {
		return err
	}

	txID, err := s.vm.IssueTx(tx.Bytes())
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.TxID = txID
	reply.ChangeAddr, err = s.vm.FormatLocalAddress(changeAddr)
	return err
}

// ImportArgs are arguments for passing into Import requests
type ImportArgs struct {
	// User that controls To
	api.UserPass

	// Chain the funds are coming from
	SourceChain string `json:"sourceChain"`

	// Address receiving the imported AVAX
	To string `json:"to"`
}

// Import imports an asset to this chain from the P/C-Chain.
// The AVAX must have already been exported from the P/C-Chain.
// Returns the ID of the newly created atomic transaction
func (s *Service) Import(_ *http.Request, args *ImportArgs, reply *api.JSONTxID) error {
	s.vm.ctx.Log.Debug("AVM: Import called",
		logging.UserString("username", args.Username),
	)

	chainID, err := s.vm.ctx.BCLookup.Lookup(args.SourceChain)
	if err != nil {
		return fmt.Errorf("problem parsing chainID %q: %w", args.SourceChain, err)
	}

	to, err := avax.ParseServiceAddress(s.vm, args.To)
	if err != nil {
		return fmt.Errorf("problem parsing to address %q: %w", args.To, err)
	}

	utxos, kc, err := s.vm.LoadUser(args.Username, args.Password, nil)
	if err != nil {
		return err
	}

	atomicUTXOs, _, _, err := s.vm.GetAtomicUTXOs(chainID, kc.Addrs, ids.ShortEmpty, ids.Empty, int(maxPageSize))
	if err != nil {
		return fmt.Errorf("problem retrieving user's atomic UTXOs: %w", err)
	}

	amountsSpent, importInputs, importKeys, err := s.vm.SpendAll(atomicUTXOs, kc)
	if err != nil {
		return err
	}

	ins := []*avax.TransferableInput{}
	keys := [][]*secp256k1.PrivateKey{}

	if amountSpent := amountsSpent[s.vm.feeAssetID]; amountSpent < s.vm.TxFee {
		var localAmountsSpent map[ids.ID]uint64
		localAmountsSpent, ins, keys, err = s.vm.Spend(
			utxos,
			kc,
			map[ids.ID]uint64{
				s.vm.feeAssetID: s.vm.TxFee - amountSpent,
			},
		)
		if err != nil {
			return err
		}
		for asset, amount := range localAmountsSpent {
			newAmount, err := safemath.Add64(amountsSpent[asset], amount)
			if err != nil {
				return fmt.Errorf("problem calculating required spend amount: %w", err)
			}
			amountsSpent[asset] = newAmount
		}
	}

	// Because we ensured that we had enough inputs for the fee, we can
	// safely just remove it without concern for underflow.
	amountsSpent[s.vm.feeAssetID] -= s.vm.TxFee

	keys = append(keys, importKeys...)

	outs := []*avax.TransferableOutput{}
	for assetID, amount := range amountsSpent {
		if amount > 0 {
			outs = append(outs, &avax.TransferableOutput{
				Asset: avax.Asset{ID: assetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: amount,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{to},
					},
				},
			})
		}
	}
	avax.SortTransferableOutputs(outs, s.vm.parser.Codec())

	tx := txs.Tx{Unsigned: &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    s.vm.ctx.NetworkID,
			BlockchainID: s.vm.ctx.ChainID,
			Outs:         outs,
			Ins:          ins,
		}},
		SourceChain: chainID,
		ImportedIns: importInputs,
	}}
	if err := tx.SignSECP256K1Fx(s.vm.parser.Codec(), keys); err != nil {
		return err
	}

	txID, err := s.vm.IssueTx(tx.Bytes())
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.TxID = txID
	return nil
}

// ExportArgs are arguments for passing into ExportAVA requests
type ExportArgs struct {
	// User, password, from addrs, change addr
	api.JSONSpendHeader
	// Amount of nAVAX to send
	Amount json.Uint64 `json:"amount"`

	// Chain the funds are going to. Optional. Used if To address does not include the chainID.
	TargetChain string `json:"targetChain"`

	// ID of the address that will receive the AVAX. This address may include the
	// chainID, which is used to determine what the destination chain is.
	To string `json:"to"`

	AssetID string `json:"assetID"`
}

// Export sends an asset from this chain to the P/C-Chain.
// After this tx is accepted, the AVAX must be imported to the P/C-chain with an importTx.
// Returns the ID of the newly created atomic transaction
func (s *Service) Export(_ *http.Request, args *ExportArgs, reply *api.JSONTxIDChangeAddr) error {
	s.vm.ctx.Log.Debug("AVM: Export called",
		logging.UserString("username", args.Username),
	)

	// Parse the asset ID
	assetID, err := s.vm.lookupAssetID(args.AssetID)
	if err != nil {
		return err
	}

	// Get the chainID and parse the to address
	chainID, to, err := s.vm.ParseAddress(args.To)
	if err != nil {
		chainID, err = s.vm.ctx.BCLookup.Lookup(args.TargetChain)
		if err != nil {
			return err
		}
		to, err = ids.ShortFromString(args.To)
		if err != nil {
			return err
		}
	}

	if args.Amount == 0 {
		return errZeroAmount
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.vm, args.From)
	if err != nil {
		return err
	}

	// Get the UTXOs/keys for the from addresses
	utxos, kc, err := s.vm.LoadUser(args.Username, args.Password, fromAddrs)
	if err != nil {
		return err
	}

	// Parse the change address.
	if len(kc.Keys) == 0 {
		return errNoKeys
	}
	changeAddr, err := s.vm.selectChangeAddr(kc.Keys[0].PublicKey().Address(), args.ChangeAddr)
	if err != nil {
		return err
	}

	amounts := map[ids.ID]uint64{}
	if assetID == s.vm.feeAssetID {
		amountWithFee, err := safemath.Add64(uint64(args.Amount), s.vm.TxFee)
		if err != nil {
			return fmt.Errorf("problem calculating required spend amount: %w", err)
		}
		amounts[s.vm.feeAssetID] = amountWithFee
	} else {
		amounts[s.vm.feeAssetID] = s.vm.TxFee
		amounts[assetID] = uint64(args.Amount)
	}

	amountsSpent, ins, keys, err := s.vm.Spend(utxos, kc, amounts)
	if err != nil {
		return err
	}

	exportOuts := []*avax.TransferableOutput{{
		Asset: avax.Asset{ID: assetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: uint64(args.Amount),
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  0,
				Threshold: 1,
				Addrs:     []ids.ShortID{to},
			},
		},
	}}

	outs := []*avax.TransferableOutput{}
	for assetID, amountSpent := range amountsSpent {
		amountToSend := amounts[assetID]
		if amountSpent > amountToSend {
			outs = append(outs, &avax.TransferableOutput{
				Asset: avax.Asset{ID: assetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: amountSpent - amountToSend,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{changeAddr},
					},
				},
			})
		}
	}
	avax.SortTransferableOutputs(outs, s.vm.parser.Codec())

	tx := txs.Tx{Unsigned: &txs.ExportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    s.vm.ctx.NetworkID,
			BlockchainID: s.vm.ctx.ChainID,
			Outs:         outs,
			Ins:          ins,
		}},
		DestinationChain: chainID,
		ExportedOuts:     exportOuts,
	}}
	if err := tx.SignSECP256K1Fx(s.vm.parser.Codec(), keys); err != nil {
		return err
	}

	txID, err := s.vm.IssueTx(tx.Bytes())
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.TxID = txID
	reply.ChangeAddr, err = s.vm.FormatLocalAddress(changeAddr)
	return err
}
