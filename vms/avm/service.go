// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/keystore"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	avajson "github.com/ava-labs/avalanchego/utils/json"
	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	// Max number of addresses that can be passed in as argument to GetUTXOs
	maxGetUTXOsAddrs = 1024

	// Max number of items allowed in a page
	maxPageSize uint64 = 1024
)

var (
	errTxNotCreateAsset   = errors.New("transaction doesn't create an asset")
	errNoMinters          = errors.New("no minters provided")
	errNoHoldersOrMinters = errors.New("no minters or initialHolders provided")
	errZeroAmount         = errors.New("amount must be positive")
	errNoOutputs          = errors.New("no outputs to send")
	errInvalidMintAmount  = errors.New("amount minted must be positive")
	errNilTxID            = errors.New("nil transaction ID")
	errNoAddresses        = errors.New("no addresses provided")
	errNoKeys             = errors.New("from addresses have no keys or funds")
	errMissingPrivateKey  = errors.New("argument 'privateKey' not given")
	errNotLinearized      = errors.New("chain is not linearized")
)

// FormattedAssetID defines a JSON formatted struct containing an assetID as a string
type FormattedAssetID struct {
	AssetID ids.ID `json:"assetID"`
}

// Service defines the base service for the asset vm
type Service struct{ vm *VM }

// GetBlock returns the requested block.
func (s *Service) GetBlock(_ *http.Request, args *api.GetBlockArgs, reply *api.GetBlockResponse) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "avm"),
		zap.String("method", "getBlock"),
		zap.Stringer("blkID", args.BlockID),
		zap.Stringer("encoding", args.Encoding),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	if s.vm.chainManager == nil {
		return errNotLinearized
	}
	block, err := s.vm.chainManager.GetStatelessBlock(args.BlockID)
	if err != nil {
		return fmt.Errorf("couldn't get block with id %s: %w", args.BlockID, err)
	}
	reply.Encoding = args.Encoding

	var result any
	if args.Encoding == formatting.JSON {
		block.InitCtx(s.vm.ctx)
		for _, tx := range block.Txs() {
			err := tx.Unsigned.Visit(&txInit{
				tx:            tx,
				ctx:           s.vm.ctx,
				typeToFxIndex: s.vm.typeToFxIndex,
				fxs:           s.vm.fxs,
			})
			if err != nil {
				return err
			}
		}
		result = block
	} else {
		result, err = formatting.Encode(args.Encoding, block.Bytes())
		if err != nil {
			return fmt.Errorf("couldn't encode block %s as string: %w", args.BlockID, err)
		}
	}

	reply.Block, err = json.Marshal(result)
	return err
}

// GetBlockByHeight returns the block at the given height.
func (s *Service) GetBlockByHeight(_ *http.Request, args *api.GetBlockByHeightArgs, reply *api.GetBlockResponse) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "avm"),
		zap.String("method", "getBlockByHeight"),
		zap.Uint64("height", uint64(args.Height)),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	if s.vm.chainManager == nil {
		return errNotLinearized
	}
	reply.Encoding = args.Encoding

	blockID, err := s.vm.state.GetBlockIDAtHeight(uint64(args.Height))
	if err != nil {
		return fmt.Errorf("couldn't get block at height %d: %w", args.Height, err)
	}
	block, err := s.vm.chainManager.GetStatelessBlock(blockID)
	if err != nil {
		s.vm.ctx.Log.Error("couldn't get accepted block",
			zap.Stringer("blkID", blockID),
			zap.Error(err),
		)
		return fmt.Errorf("couldn't get block with id %s: %w", blockID, err)
	}

	var result any
	if args.Encoding == formatting.JSON {
		block.InitCtx(s.vm.ctx)
		for _, tx := range block.Txs() {
			err := tx.Unsigned.Visit(&txInit{
				tx:            tx,
				ctx:           s.vm.ctx,
				typeToFxIndex: s.vm.typeToFxIndex,
				fxs:           s.vm.fxs,
			})
			if err != nil {
				return err
			}
		}
		result = block
	} else {
		result, err = formatting.Encode(args.Encoding, block.Bytes())
		if err != nil {
			return fmt.Errorf("couldn't encode block %s as string: %w", blockID, err)
		}
	}

	reply.Block, err = json.Marshal(result)
	return err
}

// GetHeight returns the height of the last accepted block.
func (s *Service) GetHeight(_ *http.Request, _ *struct{}, reply *api.GetHeightResponse) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "avm"),
		zap.String("method", "getHeight"),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	if s.vm.chainManager == nil {
		return errNotLinearized
	}

	blockID := s.vm.state.GetLastAccepted()
	block, err := s.vm.chainManager.GetStatelessBlock(blockID)
	if err != nil {
		s.vm.ctx.Log.Error("couldn't get last accepted block",
			zap.Stringer("blkID", blockID),
			zap.Error(err),
		)
		return fmt.Errorf("couldn't get block with id %s: %w", blockID, err)
	}

	reply.Height = avajson.Uint64(block.Height())
	return nil
}

// IssueTx attempts to issue a transaction into consensus
func (s *Service) IssueTx(_ *http.Request, args *api.FormattedTx, reply *api.JSONTxID) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "avm"),
		zap.String("method", "issueTx"),
		logging.UserString("tx", args.Tx),
	)

	txBytes, err := formatting.Decode(args.Encoding, args.Tx)
	if err != nil {
		return fmt.Errorf("problem decoding transaction: %w", err)
	}

	tx, err := s.vm.parser.ParseTx(txBytes)
	if err != nil {
		s.vm.ctx.Log.Debug("failed to parse tx",
			zap.Error(err),
		)
		return err
	}

	reply.TxID, err = s.vm.issueTxFromRPC(tx)
	return err
}

// GetTxStatusReply defines the GetTxStatus replies returned from the API
type GetTxStatusReply struct {
	Status choices.Status `json:"status"`
}

type GetAddressTxsArgs struct {
	api.JSONAddress
	// Cursor used as a page index / offset
	Cursor avajson.Uint64 `json:"cursor"`
	// PageSize num of items per page
	PageSize avajson.Uint64 `json:"pageSize"`
	// AssetID defaulted to AVAX if omitted or left blank
	AssetID string `json:"assetID"`
}

type GetAddressTxsReply struct {
	TxIDs []ids.ID `json:"txIDs"`
	// Cursor used as a page index / offset
	Cursor avajson.Uint64 `json:"cursor"`
}

// GetAddressTxs returns list of transactions for a given address
func (s *Service) GetAddressTxs(_ *http.Request, args *GetAddressTxsArgs, reply *GetAddressTxsReply) error {
	cursor := uint64(args.Cursor)
	pageSize := uint64(args.PageSize)
	s.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "getAddressTxs"),
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

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

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
	reply.Cursor = avajson.Uint64(cursor + uint64(len(reply.TxIDs)))
	return nil
}

// GetTxStatus returns the status of the specified transaction
//
// Deprecated: GetTxStatus only returns Accepted or Unknown, GetTx should be
// used instead to determine if the tx was accepted.
func (s *Service) GetTxStatus(_ *http.Request, args *api.JSONTxID, reply *GetTxStatusReply) error {
	s.vm.ctx.Log.Debug("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "getTxStatus"),
		zap.Stringer("txID", args.TxID),
	)

	if args.TxID == ids.Empty {
		return errNilTxID
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	_, err := s.vm.state.GetTx(args.TxID)
	switch err {
	case nil:
		reply.Status = choices.Accepted
	case database.ErrNotFound:
		reply.Status = choices.Unknown
	default:
		return err
	}
	return nil
}

// GetTx returns the specified transaction
func (s *Service) GetTx(_ *http.Request, args *api.GetTxArgs, reply *api.GetTxReply) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "avm"),
		zap.String("method", "getTx"),
		zap.Stringer("txID", args.TxID),
	)

	if args.TxID == ids.Empty {
		return errNilTxID
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	tx, err := s.vm.state.GetTx(args.TxID)
	if err != nil {
		return err
	}
	reply.Encoding = args.Encoding

	var result any
	if args.Encoding == formatting.JSON {
		err = tx.Unsigned.Visit(&txInit{
			tx:            tx,
			ctx:           s.vm.ctx,
			typeToFxIndex: s.vm.typeToFxIndex,
			fxs:           s.vm.fxs,
		})
		result = tx
	} else {
		result, err = formatting.Encode(args.Encoding, tx.Bytes())
	}
	if err != nil {
		return err
	}

	reply.Tx, err = json.Marshal(result)
	return err
}

// GetUTXOs gets all utxos for passed in addresses
func (s *Service) GetUTXOs(_ *http.Request, args *api.GetUTXOsArgs, reply *api.GetUTXOsReply) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "avm"),
		zap.String("method", "getUTXOs"),
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

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	if sourceChain == s.vm.ctx.ChainID {
		utxos, endAddr, endUTXOID, err = avax.GetPaginatedUTXOs(
			s.vm.state,
			addrSet,
			startAddr,
			startUTXO,
			limit,
		)
	} else {
		utxos, endAddr, endUTXOID, err = avax.GetAtomicUTXOs(
			s.vm.ctx.SharedMemory,
			s.vm.parser.Codec(),
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
	reply.NumFetched = avajson.Uint64(len(utxos))
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
	Name         string        `json:"name"`
	Symbol       string        `json:"symbol"`
	Denomination avajson.Uint8 `json:"denomination"`
}

// GetAssetDescription creates an empty account with the name passed in
func (s *Service) GetAssetDescription(_ *http.Request, args *GetAssetDescriptionArgs, reply *GetAssetDescriptionReply) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "avm"),
		zap.String("method", "getAssetDescription"),
		logging.UserString("assetID", args.AssetID),
	)

	assetID, err := s.vm.lookupAssetID(args.AssetID)
	if err != nil {
		return err
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	tx, err := s.vm.state.GetTx(assetID)
	if err != nil {
		return err
	}
	createAssetTx, ok := tx.Unsigned.(*txs.CreateAssetTx)
	if !ok {
		return errTxNotCreateAsset
	}

	reply.AssetID = assetID
	reply.Name = createAssetTx.Name
	reply.Symbol = createAssetTx.Symbol
	reply.Denomination = avajson.Uint8(createAssetTx.Denomination)

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
	Balance avajson.Uint64 `json:"balance"`
	UTXOIDs []avax.UTXOID  `json:"utxoIDs"`
}

// GetBalance returns the balance of an asset held by an address.
// If ![args.IncludePartial], returns only the balance held solely
// (1 out of 1 multisig) by the address and with a locktime in the past.
// Otherwise, returned balance includes assets held only partially by the
// address, and includes balances with locktime in the future.
func (s *Service) GetBalance(_ *http.Request, args *GetBalanceArgs, reply *GetBalanceReply) error {
	s.vm.ctx.Log.Debug("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "getBalance"),
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

	addrSet := set.Of(addr)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

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
		amt, err := safemath.Add(transferable.Amount(), uint64(reply.Balance))
		if err != nil {
			return err
		}
		reply.Balance = avajson.Uint64(amt)
		reply.UTXOIDs = append(reply.UTXOIDs, utxo.UTXOID)
	}

	return nil
}

type Balance struct {
	AssetID string         `json:"asset"`
	Balance avajson.Uint64 `json:"balance"`
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
	s.vm.ctx.Log.Debug("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "getAllBalances"),
		logging.UserString("address", args.Address),
	)

	address, err := avax.ParseServiceAddress(s.vm, args.Address)
	if err != nil {
		return fmt.Errorf("problem parsing address '%s': %w", args.Address, err)
	}
	addrSet := set.Of(address)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

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
		balance, err := safemath.Add(transferable.Amount(), balance)
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
			Balance: avajson.Uint64(balances[assetID]),
		}
		i++
	}

	return nil
}

// Holder describes how much an address owns of an asset
type Holder struct {
	Amount  avajson.Uint64 `json:"amount"`
	Address string         `json:"address"`
}

// Owners describes who can perform an action
type Owners struct {
	Threshold avajson.Uint32 `json:"threshold"`
	Minters   []string       `json:"minters"`
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
	s.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "createAsset"),
		logging.UserString("name", args.Name),
		logging.UserString("symbol", args.Symbol),
		zap.Int("numInitialHolders", len(args.InitialHolders)),
		zap.Int("numMinters", len(args.MinterSets)),
	)

	tx, changeAddr, err := s.buildCreateAssetTx(args)
	if err != nil {
		return err
	}

	assetID, err := s.vm.issueTxFromRPC(tx)
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.AssetID = assetID
	reply.ChangeAddr, err = s.vm.FormatLocalAddress(changeAddr)
	return err
}

func (s *Service) buildCreateAssetTx(args *CreateAssetArgs) (*txs.Tx, ids.ShortID, error) {
	if len(args.InitialHolders) == 0 && len(args.MinterSets) == 0 {
		return nil, ids.ShortEmpty, errNoHoldersOrMinters
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.vm, args.From)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	// Get the UTXOs/keys for the from addresses
	utxos, kc, err := s.vm.LoadUser(args.Username, args.Password, fromAddrs)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	// Parse the change address.
	if len(kc.Keys) == 0 {
		return nil, ids.ShortEmpty, errNoKeys
	}
	changeAddr, err := s.vm.selectChangeAddr(kc.Keys[0].PublicKey().Address(), args.ChangeAddr)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	amountsSpent, ins, keys, err := s.vm.Spend(
		utxos,
		kc,
		map[ids.ID]uint64{
			s.vm.feeAssetID: s.vm.CreateAssetTxFee,
		},
	)
	if err != nil {
		return nil, ids.ShortEmpty, err
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
			return nil, ids.ShortEmpty, err
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
			return nil, ids.ShortEmpty, err
		}
		minter.Addrs = minterAddrsSet.List()
		utils.Sort(minter.Addrs)
		initialState.Outs = append(initialState.Outs, minter)
	}

	codec := s.vm.parser.Codec()
	initialState.Sort(codec)

	tx := &txs.Tx{Unsigned: &txs.CreateAssetTx{
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
	return tx, changeAddr, tx.SignSECP256K1Fx(codec, keys)
}

// CreateFixedCapAsset returns ID of the newly created asset
func (s *Service) CreateFixedCapAsset(r *http.Request, args *CreateAssetArgs, reply *AssetIDChangeAddr) error {
	s.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "createFixedCapAsset"),
		logging.UserString("name", args.Name),
		logging.UserString("symbol", args.Symbol),
		zap.Int("numInitialHolders", len(args.InitialHolders)),
	)

	return s.CreateAsset(r, args, reply)
}

// CreateVariableCapAsset returns ID of the newly created asset
func (s *Service) CreateVariableCapAsset(r *http.Request, args *CreateAssetArgs, reply *AssetIDChangeAddr) error {
	s.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "createVariableCapAsset"),
		logging.UserString("name", args.Name),
		logging.UserString("symbol", args.Symbol),
		zap.Int("numMinters", len(args.MinterSets)),
	)

	return s.CreateAsset(r, args, reply)
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
	s.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "createNFTAsset"),
		logging.UserString("name", args.Name),
		logging.UserString("symbol", args.Symbol),
		zap.Int("numMinters", len(args.MinterSets)),
	)

	tx, changeAddr, err := s.buildCreateNFTAsset(args)
	if err != nil {
		return err
	}

	assetID, err := s.vm.issueTxFromRPC(tx)
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.AssetID = assetID
	reply.ChangeAddr, err = s.vm.FormatLocalAddress(changeAddr)
	return err
}

func (s *Service) buildCreateNFTAsset(args *CreateNFTAssetArgs) (*txs.Tx, ids.ShortID, error) {
	if len(args.MinterSets) == 0 {
		return nil, ids.ShortEmpty, errNoMinters
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.vm, args.From)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	// Get the UTXOs/keys for the from addresses
	utxos, kc, err := s.vm.LoadUser(args.Username, args.Password, fromAddrs)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	// Parse the change address.
	if len(kc.Keys) == 0 {
		return nil, ids.ShortEmpty, errNoKeys
	}
	changeAddr, err := s.vm.selectChangeAddr(kc.Keys[0].PublicKey().Address(), args.ChangeAddr)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	amountsSpent, ins, keys, err := s.vm.Spend(
		utxos,
		kc,
		map[ids.ID]uint64{
			s.vm.feeAssetID: s.vm.CreateAssetTxFee,
		},
	)
	if err != nil {
		return nil, ids.ShortEmpty, err
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
			return nil, ids.ShortEmpty, err
		}
		minter.Addrs = minterAddrsSet.List()
		utils.Sort(minter.Addrs)
		initialState.Outs = append(initialState.Outs, minter)
	}

	codec := s.vm.parser.Codec()
	initialState.Sort(codec)

	tx := &txs.Tx{Unsigned: &txs.CreateAssetTx{
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
	return tx, changeAddr, tx.SignSECP256K1Fx(codec, keys)
}

// CreateAddress creates an address for the user [args.Username]
func (s *Service) CreateAddress(_ *http.Request, args *api.UserPass, reply *api.JSONAddress) error {
	s.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "createAddress"),
		logging.UserString("username", args.Username),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

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
	s.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "listAddresses"),
		logging.UserString("username", args.Username),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

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
	s.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "exportKey"),
		logging.UserString("username", args.Username),
	)

	addr, err := avax.ParseServiceAddress(s.vm, args.Address)
	if err != nil {
		return fmt.Errorf("problem parsing address %q: %w", args.Address, err)
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

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
	s.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "importKey"),
		logging.UserString("username", args.Username),
	)

	if args.PrivateKey == nil {
		return errMissingPrivateKey
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

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
	Amount avajson.Uint64 `json:"amount"`

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
	s.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "sendMultiple"),
		logging.UserString("username", args.Username),
	)

	tx, changeAddr, err := s.buildSendMultiple(args)
	if err != nil {
		return err
	}

	txID, err := s.vm.issueTxFromRPC(tx)
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.TxID = txID
	reply.ChangeAddr, err = s.vm.FormatLocalAddress(changeAddr)
	return err
}

func (s *Service) buildSendMultiple(args *SendMultipleArgs) (*txs.Tx, ids.ShortID, error) {
	// Validate the memo field
	memoBytes := []byte(args.Memo)
	if l := len(memoBytes); l > avax.MaxMemoSize {
		return nil, ids.ShortEmpty, fmt.Errorf("max memo length is %d but provided memo field is length %d", avax.MaxMemoSize, l)
	} else if len(args.Outputs) == 0 {
		return nil, ids.ShortEmpty, errNoOutputs
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.vm, args.From)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	// Load user's UTXOs/keys
	utxos, kc, err := s.vm.LoadUser(args.Username, args.Password, fromAddrs)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	// Parse the change address.
	if len(kc.Keys) == 0 {
		return nil, ids.ShortEmpty, errNoKeys
	}
	changeAddr, err := s.vm.selectChangeAddr(kc.Keys[0].PublicKey().Address(), args.ChangeAddr)
	if err != nil {
		return nil, ids.ShortEmpty, err
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
			return nil, ids.ShortEmpty, errZeroAmount
		}
		assetID, ok := assetIDs[output.AssetID] // Asset ID of next output
		if !ok {
			assetID, err = s.vm.lookupAssetID(output.AssetID)
			if err != nil {
				return nil, ids.ShortEmpty, fmt.Errorf("couldn't find asset %s", output.AssetID)
			}
			assetIDs[output.AssetID] = assetID
		}
		currentAmount := amounts[assetID]
		newAmount, err := safemath.Add(currentAmount, uint64(output.Amount))
		if err != nil {
			return nil, ids.ShortEmpty, fmt.Errorf("problem calculating required spend amount: %w", err)
		}
		amounts[assetID] = newAmount

		// Parse the to address
		to, err := avax.ParseServiceAddress(s.vm, output.To)
		if err != nil {
			return nil, ids.ShortEmpty, fmt.Errorf("problem parsing to address %q: %w", output.To, err)
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

	amountWithFee, err := safemath.Add(amounts[s.vm.feeAssetID], s.vm.TxFee)
	if err != nil {
		return nil, ids.ShortEmpty, fmt.Errorf("problem calculating required spend amount: %w", err)
	}
	amountsWithFee[s.vm.feeAssetID] = amountWithFee

	amountsSpent, ins, keys, err := s.vm.Spend(
		utxos,
		kc,
		amountsWithFee,
	)
	if err != nil {
		return nil, ids.ShortEmpty, err
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

	codec := s.vm.parser.Codec()
	avax.SortTransferableOutputs(outs, codec)

	tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    s.vm.ctx.NetworkID,
		BlockchainID: s.vm.ctx.ChainID,
		Outs:         outs,
		Ins:          ins,
		Memo:         memoBytes,
	}}}
	return tx, changeAddr, tx.SignSECP256K1Fx(codec, keys)
}

// MintArgs are arguments for passing into Mint requests
type MintArgs struct {
	api.JSONSpendHeader                // User, password, from addrs, change addr
	Amount              avajson.Uint64 `json:"amount"`
	AssetID             string         `json:"assetID"`
	To                  string         `json:"to"`
}

// Mint issues a transaction that mints more of the asset
func (s *Service) Mint(_ *http.Request, args *MintArgs, reply *api.JSONTxIDChangeAddr) error {
	s.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "mint"),
		logging.UserString("username", args.Username),
	)

	tx, changeAddr, err := s.buildMint(args)
	if err != nil {
		return err
	}

	txID, err := s.vm.issueTxFromRPC(tx)
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.TxID = txID
	reply.ChangeAddr, err = s.vm.FormatLocalAddress(changeAddr)
	return err
}

func (s *Service) buildMint(args *MintArgs) (*txs.Tx, ids.ShortID, error) {
	s.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "mint"),
		logging.UserString("username", args.Username),
	)

	if args.Amount == 0 {
		return nil, ids.ShortEmpty, errInvalidMintAmount
	}

	assetID, err := s.vm.lookupAssetID(args.AssetID)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	to, err := avax.ParseServiceAddress(s.vm, args.To)
	if err != nil {
		return nil, ids.ShortEmpty, fmt.Errorf("problem parsing to address %q: %w", args.To, err)
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.vm, args.From)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	// Get the UTXOs/keys for the from addresses
	feeUTXOs, feeKc, err := s.vm.LoadUser(args.Username, args.Password, fromAddrs)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	// Parse the change address.
	if len(feeKc.Keys) == 0 {
		return nil, ids.ShortEmpty, errNoKeys
	}
	changeAddr, err := s.vm.selectChangeAddr(feeKc.Keys[0].PublicKey().Address(), args.ChangeAddr)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	amountsSpent, ins, keys, err := s.vm.Spend(
		feeUTXOs,
		feeKc,
		map[ids.ID]uint64{
			s.vm.feeAssetID: s.vm.TxFee,
		},
	)
	if err != nil {
		return nil, ids.ShortEmpty, err
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
		return nil, ids.ShortEmpty, err
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
		return nil, ids.ShortEmpty, err
	}
	keys = append(keys, opKeys...)

	tx := &txs.Tx{Unsigned: &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    s.vm.ctx.NetworkID,
			BlockchainID: s.vm.ctx.ChainID,
			Outs:         outs,
			Ins:          ins,
		}},
		Ops: ops,
	}}
	return tx, changeAddr, tx.SignSECP256K1Fx(s.vm.parser.Codec(), keys)
}

// SendNFTArgs are arguments for passing into SendNFT requests
type SendNFTArgs struct {
	api.JSONSpendHeader                // User, password, from addrs, change addr
	AssetID             string         `json:"assetID"`
	GroupID             avajson.Uint32 `json:"groupID"`
	To                  string         `json:"to"`
}

// SendNFT sends an NFT
func (s *Service) SendNFT(_ *http.Request, args *SendNFTArgs, reply *api.JSONTxIDChangeAddr) error {
	s.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "sendNFT"),
		logging.UserString("username", args.Username),
	)

	tx, changeAddr, err := s.buildSendNFT(args)
	if err != nil {
		return err
	}

	txID, err := s.vm.issueTxFromRPC(tx)
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.TxID = txID
	reply.ChangeAddr, err = s.vm.FormatLocalAddress(changeAddr)
	return err
}

func (s *Service) buildSendNFT(args *SendNFTArgs) (*txs.Tx, ids.ShortID, error) {
	// Parse the asset ID
	assetID, err := s.vm.lookupAssetID(args.AssetID)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	// Parse the to address
	to, err := avax.ParseServiceAddress(s.vm, args.To)
	if err != nil {
		return nil, ids.ShortEmpty, fmt.Errorf("problem parsing to address %q: %w", args.To, err)
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.vm, args.From)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	// Get the UTXOs/keys for the from addresses
	utxos, kc, err := s.vm.LoadUser(args.Username, args.Password, fromAddrs)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	// Parse the change address.
	if len(kc.Keys) == 0 {
		return nil, ids.ShortEmpty, errNoKeys
	}
	changeAddr, err := s.vm.selectChangeAddr(kc.Keys[0].PublicKey().Address(), args.ChangeAddr)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	amountsSpent, ins, secpKeys, err := s.vm.Spend(
		utxos,
		kc,
		map[ids.ID]uint64{
			s.vm.feeAssetID: s.vm.TxFee,
		},
	)
	if err != nil {
		return nil, ids.ShortEmpty, err
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
		return nil, ids.ShortEmpty, err
	}

	tx := &txs.Tx{Unsigned: &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    s.vm.ctx.NetworkID,
			BlockchainID: s.vm.ctx.ChainID,
			Outs:         outs,
			Ins:          ins,
		}},
		Ops: ops,
	}}

	codec := s.vm.parser.Codec()
	if err := tx.SignSECP256K1Fx(codec, secpKeys); err != nil {
		return nil, ids.ShortEmpty, err
	}
	return tx, changeAddr, tx.SignNFTFx(codec, nftKeys)
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
	s.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "mintNFT"),
		logging.UserString("username", args.Username),
	)

	tx, changeAddr, err := s.buildMintNFT(args)
	if err != nil {
		return err
	}

	txID, err := s.vm.issueTxFromRPC(tx)
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.TxID = txID
	reply.ChangeAddr, err = s.vm.FormatLocalAddress(changeAddr)
	return err
}

func (s *Service) buildMintNFT(args *MintNFTArgs) (*txs.Tx, ids.ShortID, error) {
	assetID, err := s.vm.lookupAssetID(args.AssetID)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	to, err := avax.ParseServiceAddress(s.vm, args.To)
	if err != nil {
		return nil, ids.ShortEmpty, fmt.Errorf("problem parsing to address %q: %w", args.To, err)
	}

	payloadBytes, err := formatting.Decode(args.Encoding, args.Payload)
	if err != nil {
		return nil, ids.ShortEmpty, fmt.Errorf("problem decoding payload bytes: %w", err)
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.vm, args.From)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	// Get the UTXOs/keys for the from addresses
	feeUTXOs, feeKc, err := s.vm.LoadUser(args.Username, args.Password, fromAddrs)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	// Parse the change address.
	if len(feeKc.Keys) == 0 {
		return nil, ids.ShortEmpty, errNoKeys
	}
	changeAddr, err := s.vm.selectChangeAddr(feeKc.Keys[0].PublicKey().Address(), args.ChangeAddr)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	amountsSpent, ins, secpKeys, err := s.vm.Spend(
		feeUTXOs,
		feeKc,
		map[ids.ID]uint64{
			s.vm.feeAssetID: s.vm.TxFee,
		},
	)
	if err != nil {
		return nil, ids.ShortEmpty, err
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
		return nil, ids.ShortEmpty, err
	}

	ops, nftKeys, err := s.vm.MintNFT(
		utxos,
		kc,
		assetID,
		payloadBytes,
		to,
	)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	tx := &txs.Tx{Unsigned: &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    s.vm.ctx.NetworkID,
			BlockchainID: s.vm.ctx.ChainID,
			Outs:         outs,
			Ins:          ins,
		}},
		Ops: ops,
	}}

	codec := s.vm.parser.Codec()
	if err := tx.SignSECP256K1Fx(codec, secpKeys); err != nil {
		return nil, ids.ShortEmpty, err
	}
	return tx, changeAddr, tx.SignNFTFx(codec, nftKeys)
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
	s.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "import"),
		logging.UserString("username", args.Username),
	)

	tx, err := s.buildImport(args)
	if err != nil {
		return err
	}

	txID, err := s.vm.issueTxFromRPC(tx)
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.TxID = txID
	return nil
}

func (s *Service) buildImport(args *ImportArgs) (*txs.Tx, error) {
	chainID, err := s.vm.ctx.BCLookup.Lookup(args.SourceChain)
	if err != nil {
		return nil, fmt.Errorf("problem parsing chainID %q: %w", args.SourceChain, err)
	}

	to, err := avax.ParseServiceAddress(s.vm, args.To)
	if err != nil {
		return nil, fmt.Errorf("problem parsing to address %q: %w", args.To, err)
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	utxos, kc, err := s.vm.LoadUser(args.Username, args.Password, nil)
	if err != nil {
		return nil, err
	}

	atomicUTXOs, _, _, err := avax.GetAtomicUTXOs(
		s.vm.ctx.SharedMemory,
		s.vm.parser.Codec(),
		chainID,
		kc.Addrs,
		ids.ShortEmpty,
		ids.Empty,
		int(maxPageSize),
	)
	if err != nil {
		return nil, fmt.Errorf("problem retrieving user's atomic UTXOs: %w", err)
	}

	amountsSpent, importInputs, importKeys, err := s.vm.SpendAll(atomicUTXOs, kc)
	if err != nil {
		return nil, err
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
			return nil, err
		}
		for asset, amount := range localAmountsSpent {
			newAmount, err := safemath.Add(amountsSpent[asset], amount)
			if err != nil {
				return nil, fmt.Errorf("problem calculating required spend amount: %w", err)
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

	tx := &txs.Tx{Unsigned: &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    s.vm.ctx.NetworkID,
			BlockchainID: s.vm.ctx.ChainID,
			Outs:         outs,
			Ins:          ins,
		}},
		SourceChain: chainID,
		ImportedIns: importInputs,
	}}
	return tx, tx.SignSECP256K1Fx(s.vm.parser.Codec(), keys)
}

// ExportArgs are arguments for passing into ExportAVA requests
type ExportArgs struct {
	// User, password, from addrs, change addr
	api.JSONSpendHeader
	// Amount of nAVAX to send
	Amount avajson.Uint64 `json:"amount"`

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
	s.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "avm"),
		zap.String("method", "export"),
		logging.UserString("username", args.Username),
	)

	tx, changeAddr, err := s.buildExport(args)
	if err != nil {
		return err
	}

	txID, err := s.vm.issueTxFromRPC(tx)
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.TxID = txID
	reply.ChangeAddr, err = s.vm.FormatLocalAddress(changeAddr)
	return err
}

func (s *Service) buildExport(args *ExportArgs) (*txs.Tx, ids.ShortID, error) {
	// Parse the asset ID
	assetID, err := s.vm.lookupAssetID(args.AssetID)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	// Get the chainID and parse the to address
	chainID, to, err := s.vm.ParseAddress(args.To)
	if err != nil {
		chainID, err = s.vm.ctx.BCLookup.Lookup(args.TargetChain)
		if err != nil {
			return nil, ids.ShortEmpty, err
		}
		to, err = ids.ShortFromString(args.To)
		if err != nil {
			return nil, ids.ShortEmpty, err
		}
	}

	if args.Amount == 0 {
		return nil, ids.ShortEmpty, errZeroAmount
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.vm, args.From)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	// Get the UTXOs/keys for the from addresses
	utxos, kc, err := s.vm.LoadUser(args.Username, args.Password, fromAddrs)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	// Parse the change address.
	if len(kc.Keys) == 0 {
		return nil, ids.ShortEmpty, errNoKeys
	}
	changeAddr, err := s.vm.selectChangeAddr(kc.Keys[0].PublicKey().Address(), args.ChangeAddr)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	amounts := map[ids.ID]uint64{}
	if assetID == s.vm.feeAssetID {
		amountWithFee, err := safemath.Add(uint64(args.Amount), s.vm.TxFee)
		if err != nil {
			return nil, ids.ShortEmpty, fmt.Errorf("problem calculating required spend amount: %w", err)
		}
		amounts[s.vm.feeAssetID] = amountWithFee
	} else {
		amounts[s.vm.feeAssetID] = s.vm.TxFee
		amounts[assetID] = uint64(args.Amount)
	}

	amountsSpent, ins, keys, err := s.vm.Spend(utxos, kc, amounts)
	if err != nil {
		return nil, ids.ShortEmpty, err
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

	codec := s.vm.parser.Codec()
	avax.SortTransferableOutputs(outs, codec)

	tx := &txs.Tx{Unsigned: &txs.ExportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    s.vm.ctx.NetworkID,
			BlockchainID: s.vm.ctx.ChainID,
			Outs:         outs,
			Ins:          ins,
		}},
		DestinationChain: chainID,
		ExportedOuts:     exportOuts,
	}}
	return tx, changeAddr, tx.SignSECP256K1Fx(codec, keys)
}
