// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

// test constants
const (
	GenesisTestAddr = "0x751a0b96e1042bee789452ecb20253fba40dbe85"
	GenesisTestKey  = "0xabd71b35d559563fea757f0f5edbde286fb8c043105b15abb7cd57189306d7d1"
)

// Web3API offers helper API methods
type Web3API struct{}

// ClientVersion returns the version of the vm running
func (s *Web3API) ClientVersion() string { return Version }

// Sha3 returns the bytes returned by hashing [input] with Keccak256
func (s *Web3API) Sha3(input hexutil.Bytes) hexutil.Bytes { return ethcrypto.Keccak256(input) }

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
	blk := api.vm.chain.LastAcceptedBlock()
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
