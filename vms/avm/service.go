// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
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
	errTxNotCreateAsset = errors.New("transaction doesn't create an asset")
	errNilTxID          = errors.New("nil transaction ID")
	errNoAddresses      = errors.New("no addresses provided")
	errNotLinearized    = errors.New("chain is not linearized")
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

type GetTxFeeReply struct {
	TxFee            avajson.Uint64 `json:"txFee"`
	CreateAssetTxFee avajson.Uint64 `json:"createAssetTxFee"`
}

func (s *Service) GetTxFee(_ *http.Request, _ *struct{}, reply *GetTxFeeReply) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "avm"),
		zap.String("method", "getTxFee"),
	)

	reply.TxFee = avajson.Uint64(s.vm.TxFee)
	reply.CreateAssetTxFee = avajson.Uint64(s.vm.CreateAssetTxFee)
	return nil
}
