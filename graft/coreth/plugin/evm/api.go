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
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/client"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap3"
	"github.com/ethereum/go-ethereum/common"
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
	errNoAddresses   = errors.New("no addresses provided")
	errNoSourceChain = errors.New("no source chain provided")
	errNilTxID       = errors.New("nil transaction ID")

	initialBaseFee = big.NewInt(ap3.InitialBaseFee)
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

type VersionReply struct {
	Version string `json:"version"`
}

// ClientVersion returns the version of the VM running
func (service *AvaxAPI) Version(r *http.Request, _ *struct{}, reply *VersionReply) error {
	reply.Version = Version
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
		b, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
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

	tx := &atomic.Tx{}
	if _, err := atomic.Codec.Unmarshal(txBytes, tx); err != nil {
		return fmt.Errorf("problem parsing transaction: %w", err)
	}
	if err := tx.Sign(atomic.Codec, nil); err != nil {
		return fmt.Errorf("problem initializing transaction: %w", err)
	}

	response.TxID = tx.ID()

	service.vm.ctx.Lock.Lock()
	defer service.vm.ctx.Lock.Unlock()

	if err := service.vm.mempool.AddLocalTx(tx); err != nil {
		return err
	}
	service.vm.atomicTxPushGossiper.Add(&atomic.GossipAtomicTx{Tx: tx})
	return nil
}

// GetAtomicTxStatus returns the status of the specified transaction
func (service *AvaxAPI) GetAtomicTxStatus(r *http.Request, args *api.JSONTxID, reply *client.GetAtomicTxStatusReply) error {
	log.Info("EVM: GetAtomicTxStatus called", "txID", args.TxID)

	if args.TxID == ids.Empty {
		return errNilTxID
	}

	service.vm.ctx.Lock.Lock()
	defer service.vm.ctx.Lock.Unlock()

	_, status, height, _ := service.vm.getAtomicTx(args.TxID)

	reply.Status = status
	if status == atomic.Accepted {
		// Since chain state updates run asynchronously with VM block acceptance,
		// avoid returning [Accepted] until the chain state reaches the block
		// containing the atomic tx.
		lastAccepted := service.vm.blockChain.LastAcceptedBlock()
		if height > lastAccepted.NumberU64() {
			reply.Status = atomic.Processing
			return nil
		}

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

	if status == atomic.Unknown {
		return fmt.Errorf("could not find tx %s", args.TxID)
	}

	txBytes, err := formatting.Encode(args.Encoding, tx.SignedBytes())
	if err != nil {
		return err
	}
	reply.Tx = txBytes
	reply.Encoding = args.Encoding
	if status == atomic.Accepted {
		// Since chain state updates run asynchronously with VM block acceptance,
		// avoid returning [Accepted] until the chain state reaches the block
		// containing the atomic tx.
		lastAccepted := service.vm.blockChain.LastAcceptedBlock()
		if height > lastAccepted.NumberU64() {
			return nil
		}

		jsonHeight := json.Uint64(height)
		reply.BlockHeight = &jsonHeight
	}
	return nil
}
