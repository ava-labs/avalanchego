// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/ava-labs/coreth"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/avalanche-go/api"
	"github.com/ava-labs/avalanche-go/utils/constants"
	"github.com/ava-labs/avalanche-go/utils/crypto"
	"github.com/ava-labs/avalanche-go/utils/formatting"
	"github.com/ava-labs/avalanche-go/utils/json"
	"github.com/ava-labs/go-ethereum/common"
	"github.com/ava-labs/go-ethereum/common/hexutil"
	ethcrypto "github.com/ava-labs/go-ethereum/crypto"
)

const (
	version = "Athereum 1.0"
)

// test constants
const (
	GenesisTestAddr = "0x751a0b96e1042bee789452ecb20253fba40dbe85"
	GenesisTestKey  = "0xabd71b35d559563fea757f0f5edbde286fb8c043105b15abb7cd57189306d7d1"
)

// DebugAPI introduces helper functions for debuging
type DebugAPI struct{ vm *VM }

// SnowmanAPI introduces snowman specific functionality to the evm
type SnowmanAPI struct{ vm *VM }

// NetAPI offers network related API methods
type NetAPI struct{ vm *VM }

// AvaxAPI offers Avalanche network related API methods
type AvaxAPI struct{ vm *VM }

// NewNetAPI creates a new net API instance.
func NewNetAPI(vm *VM) *NetAPI { return &NetAPI{vm} }

// Listening returns an indication if the node is listening for network connections.
func (s *NetAPI) Listening() bool { return true } // always listening

// PeerCount returns the number of connected peers
func (s *NetAPI) PeerCount() hexutil.Uint { return hexutil.Uint(0) } // TODO: report number of connected peers

// Version returns the current ethereum protocol version.
func (s *NetAPI) Version() string { return fmt.Sprintf("%d", s.vm.networkID) }

// Web3API offers helper API methods
type Web3API struct{}

// ClientVersion returns the version of the vm running
func (s *Web3API) ClientVersion() string { return version }

// Sha3 returns the bytes returned by hashing [input] with Keccak256
func (s *Web3API) Sha3(input hexutil.Bytes) hexutil.Bytes { return ethcrypto.Keccak256(input) }

// GetAcceptedFrontReply defines the reply that will be sent from the
// GetAcceptedFront API call
type GetAcceptedFrontReply struct {
	Hash   common.Hash `json:"hash"`
	Number *big.Int    `json:"number"`
}

// GetAcceptedFront returns the last accepted block's hash and height
func (api *SnowmanAPI) GetAcceptedFront(ctx context.Context) (*GetAcceptedFrontReply, error) {
	blk := api.vm.getLastAccepted().ethBlock
	return &GetAcceptedFrontReply{
		Hash:   blk.Hash(),
		Number: blk.Number(),
	}, nil
}

// GetGenesisBalance returns the current funds in the genesis
func (api *DebugAPI) GetGenesisBalance(ctx context.Context) (*hexutil.Big, error) {
	lastAccepted := api.vm.getLastAccepted()
	api.vm.ctx.Log.Verbo("Currently accepted block front: %s", lastAccepted.ethBlock.Hash().Hex())
	state, err := api.vm.chain.BlockState(lastAccepted.ethBlock)
	if err != nil {
		return nil, err
	}
	return (*hexutil.Big)(state.GetBalance(common.HexToAddress(GenesisTestAddr))), nil
}

// SpendGenesis funds
func (api *DebugAPI) SpendGenesis(ctx context.Context, nonce uint64) error {
	api.vm.ctx.Log.Info("Spending the genesis")

	value := big.NewInt(1000000000000)
	gasLimit := 21000
	gasPrice := big.NewInt(1000000000)

	genPrivateKey, err := ethcrypto.HexToECDSA(GenesisTestKey[2:])
	if err != nil {
		return err
	}
	bob, err := coreth.NewKey(rand.Reader)
	if err != nil {
		return err
	}

	tx := types.NewTransaction(nonce, bob.Address, value, uint64(gasLimit), gasPrice, nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(api.vm.chainID), genPrivateKey)
	if err != nil {
		return err
	}

	if err := api.vm.issueRemoteTxs([]*types.Transaction{signedTx}); err != nil {
		return err
	}

	return nil
}

// IssueBlock to the chain
func (api *DebugAPI) IssueBlock(ctx context.Context) error {
	api.vm.ctx.Log.Info("Issuing a new block")

	return api.vm.tryBlockGen()
}

// ExportKeyArgs are arguments for ExportKey
type ExportKeyArgs struct {
	api.UserPass
	Address string `json:"address"`
}

// ExportKeyReply is the response for ExportKey
type ExportKeyReply struct {
	// The decrypted PrivateKey for the Address provided in the arguments
	PrivateKey string `json:"privateKey"`
}

// ExportKey returns a private key from the provided user
func (service *AvaxAPI) ExportKey(r *http.Request, args *ExportKeyArgs, reply *ExportKeyReply) error {
	service.vm.ctx.Log.Info("Platform: ExportKey called")

	address, err := service.vm.ParseEthAddress(args.Address)
	if err != nil {
		return fmt.Errorf("couldn't parse %s to address: %s", args.Address, err)
	}

	db, err := service.vm.ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}
	defer db.Close()

	user := user{db: db}
	sk, err := user.getKey(address)
	if err != nil {
		return fmt.Errorf("problem retrieving private key: %w", err)
	}
	reply.PrivateKey = constants.SecretKeyPrefix + formatting.CB58{Bytes: sk.Bytes()}.String()
	return nil
}

// ImportKeyArgs are arguments for ImportKey
type ImportKeyArgs struct {
	api.UserPass
	PrivateKey string `json:"privateKey"`
}

// ImportKey adds a private key to the provided user
func (service *AvaxAPI) ImportKey(r *http.Request, args *ImportKeyArgs, reply *api.JsonAddress) error {
	service.vm.ctx.Log.Info("Platform: ImportKey called for user '%s'", args.Username)

	if !strings.HasPrefix(args.PrivateKey, constants.SecretKeyPrefix) {
		return fmt.Errorf("private key missing %s prefix", constants.SecretKeyPrefix)
	}

	trimmedPrivateKey := strings.TrimPrefix(args.PrivateKey, constants.SecretKeyPrefix)
	formattedPrivateKey := formatting.CB58{}
	if err := formattedPrivateKey.FromString(trimmedPrivateKey); err != nil {
		return fmt.Errorf("problem parsing private key: %w", err)
	}

	factory := crypto.FactorySECP256K1R{}
	skIntf, err := factory.ToPrivateKey(formattedPrivateKey.Bytes)
	if err != nil {
		return fmt.Errorf("problem parsing private key: %w", err)
	}
	sk := skIntf.(*crypto.PrivateKeySECP256K1R)

	// TODO: return eth address here
	reply.Address, err = service.vm.FormatEthAddress(GetEthAddress(sk))
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}

	db, err := service.vm.ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving data: %w", err)
	}
	defer db.Close()

	user := user{db: db}
	if err := user.putAddress(sk); err != nil {
		return fmt.Errorf("problem saving key %w", err)
	}
	return nil
}

// ImportAVAXArgs are the arguments to ImportAVAX
type ImportAVAXArgs struct {
	api.UserPass

	// Chain the funds are coming from
	SourceChain string `json:"sourceChain"`

	// The address that will receive the imported funds
	To string `json:"to"`
}

// ImportAVAX issues a transaction to import AVAX from the X-chain. The AVAX
// must have already been exported from the X-Chain.
func (service *AvaxAPI) ImportAVAX(_ *http.Request, args *ImportAVAXArgs, response *api.JsonTxID) error {
	service.vm.ctx.Log.Info("Platform: ImportAVAX called")

	chainID, err := service.vm.ctx.BCLookup.Lookup(args.SourceChain)
	if err != nil {
		return fmt.Errorf("problem parsing chainID %q: %w", args.SourceChain, err)
	}

	to, err := service.vm.ParseEthAddress(args.To)
	if err != nil { // Parse address
		return fmt.Errorf("couldn't parse argument 'to' to an address: %w", err)
	}

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

	tx, err := service.vm.newImportTx(chainID, to, privKeys)
	if err != nil {
		return err
	}

	response.TxID = tx.ID()
	return service.vm.issueTx(tx)
}

// ExportAVAXArgs are the arguments to ExportAVAX
type ExportAVAXArgs struct {
	api.UserPass

	// Amount of AVAX to send
	Amount json.Uint64 `json:"amount"`

	// ID of the address that will receive the AVAX. This address includes the
	// chainID, which is used to determine what the destination chain is.
	To string `json:"to"`
}

// ExportAVAX exports AVAX from the P-Chain to the X-Chain
// It must be imported on the X-Chain to complete the transfer
func (service *AvaxAPI) ExportAVAX(_ *http.Request, args *ExportAVAXArgs, response *api.JsonTxID) error {
	service.vm.ctx.Log.Info("Platform: ExportAVAX called")

	if args.Amount == 0 {
		return errors.New("argument 'amount' must be > 0")
	}

	chainID, to, err := service.vm.ParseAddress(args.To)
	if err != nil {
		return err
	}

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

	// Create the transaction
	tx, err := service.vm.newExportTx(
		uint64(args.Amount), // Amount
		chainID,             // ID of the chain to send the funds to
		to,                  // Address
		privKeys,            // Private keys
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	response.TxID = tx.ID()
	return service.vm.issueTx(tx)
}
