// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/log"
)

// test constants
const (
	GenesisTestAddr = "0x751a0b96e1042bee789452ecb20253fba40dbe85"
	GenesisTestKey  = "0xabd71b35d559563fea757f0f5edbde286fb8c043105b15abb7cd57189306d7d1"

	// Max number of addresses that can be passed in as argument to GetUTXOs
	maxGetUTXOsAddrs = 1024
)

var (
	errNoAddresses       = errors.New("no addresses provided")
	errNoSourceChain     = errors.New("no source chain provided")
	errNilTxID           = errors.New("nil transaction ID")
	errMissingPrivateKey = errors.New("argument 'privateKey' not given")

	initialBaseFee = big.NewInt(params.ApricotPhase3InitialBaseFee)
)

// SnowmanAPI introduces snowman specific functionality to the evm
type SnowmanAPI struct{ vm *VM }

// GetAcceptedFrontReply defines the reply that will be sent from the
// GetAcceptedFront API call
type GetAcceptedFrontReply struct {
	Hash   common.Hash `json:"hash"`
	Number *big.Int    `json:"number"`
}

// GetAcceptedFront returns the last accepted block's hash and height
func (api *SnowmanAPI) GetAcceptedFront(ctx context.Context) (*GetAcceptedFrontReply, error) {
	blk := api.vm.blockChain.LastConsensusAcceptedBlock()
	return &GetAcceptedFrontReply{
		Hash:   blk.Hash(),
		Number: blk.Number(),
	}, nil
}

// IssueBlock to the chain
func (api *SnowmanAPI) IssueBlock(ctx context.Context) error {
	log.Info("Issuing a new block")

	api.vm.builder.signalTxsReady()
	return nil
}

// AvaxAPI offers Avalanche network related API methods
type AvaxAPI struct{ vm *VM }

// parseAssetID parses an assetID string into an ID
func (service *AvaxAPI) parseAssetID(assetID string) (ids.ID, error) {
	if assetID == "" {
		return ids.ID{}, fmt.Errorf("assetID is required")
	} else if assetID == "AVAX" {
		return service.vm.ctx.AVAXAssetID, nil
	} else {
		return ids.FromString(assetID)
	}
}

type VersionReply struct {
	Version string `json:"version"`
}

// ClientVersion returns the version of the VM running
func (service *AvaxAPI) Version(r *http.Request, _ *struct{}, reply *VersionReply) error {
	reply.Version = Version
	return nil
}

// ExportKeyArgs are arguments for ExportKey
type ExportKeyArgs struct {
	api.UserPass
	Address string `json:"address"`
}

// ExportKeyReply is the response for ExportKey
type ExportKeyReply struct {
	// The decrypted PrivateKey for the Address provided in the arguments
	PrivateKey    *secp256k1.PrivateKey `json:"privateKey"`
	PrivateKeyHex string                `json:"privateKeyHex"`
}

// ExportKey returns a private key from the provided user
func (service *AvaxAPI) ExportKey(r *http.Request, args *ExportKeyArgs, reply *ExportKeyReply) error {
	log.Info("EVM: ExportKey called")

	address, err := ParseEthAddress(args.Address)
	if err != nil {
		return fmt.Errorf("couldn't parse %s to address: %s", args.Address, err)
	}

	service.vm.ctx.Lock.Lock()
	defer service.vm.ctx.Lock.Unlock()

	db, err := service.vm.ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}
	defer db.Close()

	user := user{db: db}
	reply.PrivateKey, err = user.getKey(address)
	if err != nil {
		return fmt.Errorf("problem retrieving private key: %w", err)
	}
	reply.PrivateKeyHex = hexutil.Encode(reply.PrivateKey.Bytes())
	return nil
}

// ImportKeyArgs are arguments for ImportKey
type ImportKeyArgs struct {
	api.UserPass
	PrivateKey *secp256k1.PrivateKey `json:"privateKey"`
}

// ImportKey adds a private key to the provided user
func (service *AvaxAPI) ImportKey(r *http.Request, args *ImportKeyArgs, reply *api.JSONAddress) error {
	log.Info("EVM: ImportKey called", "username", args.Username)

	if args.PrivateKey == nil {
		return errMissingPrivateKey
	}

	reply.Address = GetEthAddress(args.PrivateKey).Hex()

	service.vm.ctx.Lock.Lock()
	defer service.vm.ctx.Lock.Unlock()

	db, err := service.vm.ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving data: %w", err)
	}
	defer db.Close()

	user := user{db: db}
	if err := user.putAddress(args.PrivateKey); err != nil {
		return fmt.Errorf("problem saving key %w", err)
	}
	return nil
}

// ImportArgs are arguments for passing into Import requests
type ImportArgs struct {
	api.UserPass

	// Fee that should be used when creating the tx
	BaseFee *hexutil.Big `json:"baseFee"`

	// Chain the funds are coming from
	SourceChain string `json:"sourceChain"`

	// The address that will receive the imported funds
	To common.Address `json:"to"`
}

// ImportAVAX is a deprecated name for Import.
func (service *AvaxAPI) ImportAVAX(_ *http.Request, args *ImportArgs, response *api.JSONTxID) error {
	return service.Import(nil, args, response)
}

// Import issues a transaction to import AVAX from the X-chain. The AVAX
// must have already been exported from the X-Chain.
func (service *AvaxAPI) Import(_ *http.Request, args *ImportArgs, response *api.JSONTxID) error {
	log.Info("EVM: ImportAVAX called")

	chainID, err := service.vm.ctx.BCLookup.Lookup(args.SourceChain)
	if err != nil {
		return fmt.Errorf("problem parsing chainID %q: %w", args.SourceChain, err)
	}

	service.vm.ctx.Lock.Lock()
	defer service.vm.ctx.Lock.Unlock()

	// Get the user's info
	db, err := service.vm.ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("couldn't get user '%s': %w", args.Username, err)
	}
	defer db.Close()

	user := user{db: db}
	privKeys, err := user.getKeys()
	if err != nil { // Get keys
		return fmt.Errorf("couldn't get keys controlled by the user: %w", err)
	}

	var baseFee *big.Int
	if args.BaseFee == nil {
		// Get the base fee to use
		baseFee, err = service.vm.estimateBaseFee(context.Background())
		if err != nil {
			return err
		}
	} else {
		baseFee = args.BaseFee.ToInt()
	}

	tx, err := service.vm.newImportTx(chainID, args.To, baseFee, privKeys)
	if err != nil {
		return err
	}

	response.TxID = tx.ID()
	if err := service.vm.mempool.AddLocalTx(tx); err != nil {
		return err
	}
	service.vm.atomicTxPushGossiper.Add(&GossipAtomicTx{tx})
	return nil
}

// ExportAVAXArgs are the arguments to ExportAVAX
type ExportAVAXArgs struct {
	api.UserPass

	// Fee that should be used when creating the tx
	BaseFee *hexutil.Big `json:"baseFee"`

	// Amount of asset to send
	Amount json.Uint64 `json:"amount"`

	// Chain the funds are going to. Optional. Used if To address does not
	// include the chainID.
	TargetChain string `json:"targetChain"`

	// ID of the address that will receive the AVAX. This address may include
	// the chainID, which is used to determine what the destination chain is.
	To string `json:"to"`
}

// ExportAVAX exports AVAX from the C-Chain to the X-Chain
// It must be imported on the X-Chain to complete the transfer
func (service *AvaxAPI) ExportAVAX(_ *http.Request, args *ExportAVAXArgs, response *api.JSONTxID) error {
	return service.Export(nil, &ExportArgs{
		ExportAVAXArgs: *args,
		AssetID:        service.vm.ctx.AVAXAssetID.String(),
	}, response)
}

// ExportArgs are the arguments to Export
type ExportArgs struct {
	ExportAVAXArgs
	// AssetID of the tokens
	AssetID string `json:"assetID"`
}

// Export exports an asset from the C-Chain to the X-Chain
// It must be imported on the X-Chain to complete the transfer
func (service *AvaxAPI) Export(_ *http.Request, args *ExportArgs, response *api.JSONTxID) error {
	log.Info("EVM: Export called")

	assetID, err := service.parseAssetID(args.AssetID)
	if err != nil {
		return err
	}

	if args.Amount == 0 {
		return errors.New("argument 'amount' must be > 0")
	}

	// Get the chainID and parse the to address
	chainID, to, err := service.vm.ParseAddress(args.To)
	if err != nil {
		chainID, err = service.vm.ctx.BCLookup.Lookup(args.TargetChain)
		if err != nil {
			return err
		}
		to, err = ids.ShortFromString(args.To)
		if err != nil {
			return err
		}
	}

	service.vm.ctx.Lock.Lock()
	defer service.vm.ctx.Lock.Unlock()

	// Get this user's data
	db, err := service.vm.ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}
	defer db.Close()

	user := user{db: db}
	privKeys, err := user.getKeys()
	if err != nil {
		return fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
	}

	var baseFee *big.Int
	if args.BaseFee == nil {
		// Get the base fee to use
		baseFee, err = service.vm.estimateBaseFee(context.Background())
		if err != nil {
			return err
		}
	} else {
		baseFee = args.BaseFee.ToInt()
	}

	// Create the transaction
	tx, err := service.vm.newExportTx(
		assetID,             // AssetID
		uint64(args.Amount), // Amount
		chainID,             // ID of the chain to send the funds to
		to,                  // Address
		baseFee,
		privKeys, // Private keys
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	response.TxID = tx.ID()
	if err := service.vm.mempool.AddLocalTx(tx); err != nil {
		return err
	}
	service.vm.atomicTxPushGossiper.Add(&GossipAtomicTx{tx})
	return nil
}

// GetUTXOs gets all utxos for passed in addresses
func (service *AvaxAPI) GetUTXOs(r *http.Request, args *api.GetUTXOsArgs, reply *api.GetUTXOsReply) error {
	log.Info("EVM: GetUTXOs called", "Addresses", args.Addresses)

	if len(args.Addresses) == 0 {
		return errNoAddresses
	}
	if len(args.Addresses) > maxGetUTXOsAddrs {
		return fmt.Errorf("number of addresses given, %d, exceeds maximum, %d", len(args.Addresses), maxGetUTXOsAddrs)
	}

	if args.SourceChain == "" {
		return errNoSourceChain
	}

	chainID, err := service.vm.ctx.BCLookup.Lookup(args.SourceChain)
	if err != nil {
		return fmt.Errorf("problem parsing source chainID %q: %w", args.SourceChain, err)
	}
	sourceChain := chainID

	addrSet := set.Set[ids.ShortID]{}
	for _, addrStr := range args.Addresses {
		addr, err := service.vm.ParseServiceAddress(addrStr)
		if err != nil {
			return fmt.Errorf("couldn't parse address %q: %w", addrStr, err)
		}
		addrSet.Add(addr)
	}

	startAddr := ids.ShortEmpty
	startUTXO := ids.Empty
	if args.StartIndex.Address != "" || args.StartIndex.UTXO != "" {
		startAddr, err = service.vm.ParseServiceAddress(args.StartIndex.Address)
		if err != nil {
			return fmt.Errorf("couldn't parse start index address %q: %w", args.StartIndex.Address, err)
		}
		startUTXO, err = ids.FromString(args.StartIndex.UTXO)
		if err != nil {
			return fmt.Errorf("couldn't parse start index utxo: %w", err)
		}
	}

	service.vm.ctx.Lock.Lock()
	defer service.vm.ctx.Lock.Unlock()

	utxos, endAddr, endUTXOID, err := service.vm.GetAtomicUTXOs(
		sourceChain,
		addrSet,
		startAddr,
		startUTXO,
		int(args.Limit),
	)
	if err != nil {
		return fmt.Errorf("problem retrieving UTXOs: %w", err)
	}

	reply.UTXOs = make([]string, len(utxos))
	for i, utxo := range utxos {
		b, err := service.vm.codec.Marshal(codecVersion, utxo)
		if err != nil {
			return fmt.Errorf("problem marshalling UTXO: %w", err)
		}
		str, err := formatting.Encode(args.Encoding, b)
		if err != nil {
			return fmt.Errorf("problem encoding utxo: %w", err)
		}
		reply.UTXOs[i] = str
	}

	endAddress, err := service.vm.FormatLocalAddress(endAddr)
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}

	reply.EndIndex.Address = endAddress
	reply.EndIndex.UTXO = endUTXOID.String()
	reply.NumFetched = json.Uint64(len(utxos))
	reply.Encoding = args.Encoding
	return nil
}

func (service *AvaxAPI) IssueTx(r *http.Request, args *api.FormattedTx, response *api.JSONTxID) error {
	log.Info("EVM: IssueTx called")

	txBytes, err := formatting.Decode(args.Encoding, args.Tx)
	if err != nil {
		return fmt.Errorf("problem decoding transaction: %w", err)
	}

	tx := &Tx{}
	if _, err := service.vm.codec.Unmarshal(txBytes, tx); err != nil {
		return fmt.Errorf("problem parsing transaction: %w", err)
	}
	if err := tx.Sign(service.vm.codec, nil); err != nil {
		return fmt.Errorf("problem initializing transaction: %w", err)
	}

	response.TxID = tx.ID()

	service.vm.ctx.Lock.Lock()
	defer service.vm.ctx.Lock.Unlock()

	if err := service.vm.mempool.AddLocalTx(tx); err != nil {
		return err
	}
	service.vm.atomicTxPushGossiper.Add(&GossipAtomicTx{tx})
	return nil
}

// GetAtomicTxStatusReply defines the GetAtomicTxStatus replies returned from the API
type GetAtomicTxStatusReply struct {
	Status      Status       `json:"status"`
	BlockHeight *json.Uint64 `json:"blockHeight,omitempty"`
}

// GetAtomicTxStatus returns the status of the specified transaction
func (service *AvaxAPI) GetAtomicTxStatus(r *http.Request, args *api.JSONTxID, reply *GetAtomicTxStatusReply) error {
	log.Info("EVM: GetAtomicTxStatus called", "txID", args.TxID)

	if args.TxID == ids.Empty {
		return errNilTxID
	}

	service.vm.ctx.Lock.Lock()
	defer service.vm.ctx.Lock.Unlock()

	_, status, height, _ := service.vm.getAtomicTx(args.TxID)

	reply.Status = status
	if status == Accepted {
		jsonHeight := json.Uint64(height)
		reply.BlockHeight = &jsonHeight
	}
	return nil
}

type FormattedTx struct {
	api.FormattedTx
	BlockHeight *json.Uint64 `json:"blockHeight,omitempty"`
}

// GetAtomicTx returns the specified transaction
func (service *AvaxAPI) GetAtomicTx(r *http.Request, args *api.GetTxArgs, reply *FormattedTx) error {
	log.Info("EVM: GetAtomicTx called", "txID", args.TxID)

	if args.TxID == ids.Empty {
		return errNilTxID
	}

	service.vm.ctx.Lock.Lock()
	defer service.vm.ctx.Lock.Unlock()

	tx, status, height, err := service.vm.getAtomicTx(args.TxID)
	if err != nil {
		return err
	}

	if status == Unknown {
		return fmt.Errorf("could not find tx %s", args.TxID)
	}

	txBytes, err := formatting.Encode(args.Encoding, tx.SignedBytes())
	if err != nil {
		return err
	}
	reply.Tx = txBytes
	reply.Encoding = args.Encoding
	if status == Accepted {
		jsonHeight := json.Uint64(height)
		reply.BlockHeight = &jsonHeight
	}
	return nil
}
