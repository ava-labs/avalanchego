// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/subnet-evm/core/rawdb"
	"github.com/ava-labs/subnet-evm/core/state"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

//go:generate go run github.com/fjl/gencodec -type Genesis -field-override genesisSpecMarshaling -out gen_genesis.go
//go:generate go run github.com/fjl/gencodec -type GenesisAccount -field-override genesisAccountMarshaling -out gen_genesis_account.go

var errGenesisNoConfig = errors.New("genesis has no chain configuration")

type Airdrop struct {
	// Address strings are hex-formatted common.Address
	Address common.Address `json:"address"`
}

// Genesis specifies the header fields, state of a genesis block. It also defines hard
// fork switch-over blocks through the chain configuration.
type Genesis struct {
	Config        *params.ChainConfig `json:"config"`
	Nonce         uint64              `json:"nonce"`
	Timestamp     uint64              `json:"timestamp"`
	ExtraData     []byte              `json:"extraData"`
	GasLimit      uint64              `json:"gasLimit"   gencodec:"required"`
	Difficulty    *big.Int            `json:"difficulty" gencodec:"required"`
	Mixhash       common.Hash         `json:"mixHash"`
	Coinbase      common.Address      `json:"coinbase"`
	Alloc         GenesisAlloc        `json:"alloc"      gencodec:"required"`
	AirdropHash   common.Hash         `json:"airdropHash"`
	AirdropAmount *big.Int            `json:"airdropAmount"`
	AirdropData   []byte              `json:"-"` // provided in a separate file, not serialized in this struct.

	// These fields are used for consensus tests. Please don't use them
	// in actual genesis blocks.
	Number     uint64      `json:"number"`
	GasUsed    uint64      `json:"gasUsed"`
	ParentHash common.Hash `json:"parentHash"`
	BaseFee    *big.Int    `json:"baseFeePerGas"`
}

// GenesisAlloc specifies the initial state that is part of the genesis block.
type GenesisAlloc map[common.Address]GenesisAccount

func (ga *GenesisAlloc) UnmarshalJSON(data []byte) error {
	m := make(map[common.UnprefixedAddress]GenesisAccount)
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	*ga = make(GenesisAlloc)
	for addr, a := range m {
		(*ga)[common.Address(addr)] = a
	}
	return nil
}

// GenesisAccount is an account in the state of the genesis block.
type GenesisAccount struct {
	Code       []byte                      `json:"code,omitempty"`
	Storage    map[common.Hash]common.Hash `json:"storage,omitempty"`
	Balance    *big.Int                    `json:"balance" gencodec:"required"`
	Nonce      uint64                      `json:"nonce,omitempty"`
	PrivateKey []byte                      `json:"secretKey,omitempty"` // for tests
}

// field type overrides for gencodec
type genesisSpecMarshaling struct {
	Nonce         math.HexOrDecimal64
	Timestamp     math.HexOrDecimal64
	ExtraData     hexutil.Bytes
	GasLimit      math.HexOrDecimal64
	GasUsed       math.HexOrDecimal64
	Number        math.HexOrDecimal64
	Difficulty    *math.HexOrDecimal256
	BaseFee       *math.HexOrDecimal256
	Alloc         map[common.UnprefixedAddress]GenesisAccount
	AirdropAmount *math.HexOrDecimal256
}

type genesisAccountMarshaling struct {
	Code       hexutil.Bytes
	Balance    *math.HexOrDecimal256
	Nonce      math.HexOrDecimal64
	Storage    map[storageJSON]storageJSON
	PrivateKey hexutil.Bytes
}

// storageJSON represents a 256 bit byte array, but allows less than 256 bits when
// unmarshaling from hex.
type storageJSON common.Hash

func (h *storageJSON) UnmarshalText(text []byte) error {
	text = bytes.TrimPrefix(text, []byte("0x"))
	if len(text) > 64 {
		return fmt.Errorf("too many hex characters in storage key/value %q", text)
	}
	offset := len(h) - len(text)/2 // pad on the left
	if _, err := hex.Decode(h[offset:], text); err != nil {
		return fmt.Errorf("invalid hex storage key/value %q", text)
	}
	return nil
}

func (h storageJSON) MarshalText() ([]byte, error) {
	return hexutil.Bytes(h[:]).MarshalText()
}

// GenesisMismatchError is raised when trying to overwrite an existing
// genesis block with an incompatible one.
type GenesisMismatchError struct {
	Stored, New common.Hash
}

func (e *GenesisMismatchError) Error() string {
	return fmt.Sprintf("database contains incompatible genesis (have %x, new %x)", e.Stored, e.New)
}

// SetupGenesisBlock writes or updates the genesis block in db.
// The block that will be used is:
//
//	                     genesis == nil       genesis != nil
//	                  +------------------------------------------
//	db has no genesis |  main-net default  |  genesis
//	db has genesis    |  from DB           |  genesis (if compatible)

// The argument [genesis] must be specified and must contain a valid chain config.
// If the genesis block has already been set up, then we verify the hash matches the genesis passed in
// and that the chain config contained in genesis is backwards compatible with what is stored in the database.
//
// The stored chain configuration will be updated if it is compatible (i.e. does not
// specify a fork block below the local head block). In case of a conflict, the
// error is a *params.ConfigCompatError and the new, unwritten config is returned.
func SetupGenesisBlock(
	db ethdb.Database, triedb *trie.Database, genesis *Genesis, lastAcceptedHash common.Hash, skipChainConfigCheckCompatible bool,
) (*params.ChainConfig, common.Hash, error) {
	if genesis == nil {
		return nil, common.Hash{}, ErrNoGenesis
	}
	if genesis.Config == nil {
		return nil, common.Hash{}, errGenesisNoConfig
	}
	// Just commit the new block if there is no stored genesis block.
	stored := rawdb.ReadCanonicalHash(db, 0)
	if (stored == common.Hash{}) {
		log.Info("Writing genesis to database")
		block, err := genesis.Commit(db, triedb)
		if err != nil {
			return genesis.Config, common.Hash{}, err
		}
		return genesis.Config, block.Hash(), nil
	}
	// We have the genesis block in database but the corresponding state is missing.
	header := rawdb.ReadHeader(db, stored, 0)
	if header.Root != types.EmptyRootHash && !rawdb.HasLegacyTrieNode(db, header.Root) {
		// Ensure the stored genesis matches with the given one.
		hash := genesis.ToBlock().Hash()
		if hash != stored {
			return genesis.Config, common.Hash{}, &GenesisMismatchError{stored, hash}
		}
		_, err := genesis.Commit(db, triedb)
		return genesis.Config, common.Hash{}, err
	}
	// Check whether the genesis block is already written.
	hash := genesis.ToBlock().Hash()
	if hash != stored {
		return genesis.Config, common.Hash{}, &GenesisMismatchError{stored, hash}
	}
	// Get the existing chain configuration.
	newcfg := genesis.Config
	if err := newcfg.CheckConfigForkOrder(); err != nil {
		return newcfg, common.Hash{}, err
	}
	storedcfg := rawdb.ReadChainConfig(db, stored)
	// If there is no previously stored chain config, write the chain config to disk.
	if storedcfg == nil {
		// Note: this can happen since we did not previously write the genesis block and chain config in the same batch.
		log.Warn("Found genesis block without chain config")
		rawdb.WriteChainConfig(db, stored, newcfg)
		return newcfg, stored, nil
	}
	// Check config compatibility and write the config. Compatibility errors
	// are returned to the caller unless we're already at block zero.
	// we use last accepted block for cfg compatibility check. Note this allows
	// the node to continue if it previously halted due to attempting to process blocks with
	// an incorrect chain config.
	lastBlock := ReadBlockByHash(db, lastAcceptedHash)
	// this should never happen, but we check anyway
	// when we start syncing from scratch, the last accepted block
	// will be genesis block
	if lastBlock == nil {
		return newcfg, common.Hash{}, fmt.Errorf("missing last accepted block")
	}
	height := lastBlock.NumberU64()
	timestamp := lastBlock.Time()
	if skipChainConfigCheckCompatible {
		log.Info("skipping verifying activated network upgrades on chain config")
	} else {
		compatErr := storedcfg.CheckCompatible(newcfg, height, timestamp)
		if compatErr != nil && ((height != 0 && compatErr.RewindToBlock != 0) || (timestamp != 0 && compatErr.RewindToTime != 0)) {
			storedData, _ := storedcfg.ToWithUpgradesJSON().MarshalJSON()
			newData, _ := newcfg.ToWithUpgradesJSON().MarshalJSON()
			log.Error("found mismatch between config on database vs. new config", "storedConfig", string(storedData), "newConfig", string(newData))
			return newcfg, stored, compatErr
		}
	}
	// Required to write the chain config to disk to ensure both the chain config and upgrade bytes are persisted to disk.
	// Note: this intentionally removes an extra check from upstream.
	rawdb.WriteChainConfig(db, stored, newcfg)
	return newcfg, stored, nil
}

// ToBlock creates the genesis block and writes state of a genesis specification
// to the given database (or discards it if nil).
func (g *Genesis) ToBlock() *types.Block {
	db := rawdb.NewMemoryDatabase()
	return g.toBlock(db, trie.NewDatabase(db))
}

// TODO: migrate this function to "flush" for more similarity with upstream.
func (g *Genesis) toBlock(db ethdb.Database, triedb *trie.Database) *types.Block {
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseWithNodeDB(db, triedb), nil)
	if err != nil {
		panic(err)
	}
	if g.AirdropHash != (common.Hash{}) {
		t := time.Now()
		h := common.BytesToHash(crypto.Keccak256(g.AirdropData))
		if g.AirdropHash != h {
			panic(fmt.Sprintf("expected standard allocation %s but got %s", g.AirdropHash, h))
		}
		airdrop := []*Airdrop{}
		if err := json.Unmarshal(g.AirdropData, &airdrop); err != nil {
			panic(err)
		}
		for _, alloc := range airdrop {
			statedb.SetBalance(alloc.Address, g.AirdropAmount)
		}
		log.Debug(
			"applied airdrop allocation",
			"hash", h, "addrs", len(airdrop), "balance", g.AirdropAmount,
			"t", time.Since(t),
		)
	}

	head := &types.Header{
		Number:     new(big.Int).SetUint64(g.Number),
		Nonce:      types.EncodeNonce(g.Nonce),
		Time:       g.Timestamp,
		ParentHash: g.ParentHash,
		Extra:      g.ExtraData,
		GasLimit:   g.GasLimit,
		GasUsed:    g.GasUsed,
		BaseFee:    g.BaseFee,
		Difficulty: g.Difficulty,
		MixDigest:  g.Mixhash,
		Coinbase:   g.Coinbase,
	}

	// Configure any stateful precompiles that should be enabled in the genesis.
	err = ApplyPrecompileActivations(g.Config, nil, types.NewBlockWithHeader(head), statedb)
	if err != nil {
		panic(fmt.Sprintf("unable to configure precompiles in genesis block: %v", err))
	}

	// Do custom allocation after airdrop in case an address shows up in standard
	// allocation
	for addr, account := range g.Alloc {
		statedb.SetBalance(addr, account.Balance)
		statedb.SetCode(addr, account.Code)
		statedb.SetNonce(addr, account.Nonce)
		for key, value := range account.Storage {
			statedb.SetState(addr, key, value)
		}
	}
	root := statedb.IntermediateRoot(false)
	head.Root = root

	if g.GasLimit == 0 {
		head.GasLimit = params.GenesisGasLimit
	}
	if g.Difficulty == nil {
		head.Difficulty = params.GenesisDifficulty
	}
	if g.Config != nil && g.Config.IsSubnetEVM(0) {
		if g.BaseFee != nil {
			head.BaseFee = g.BaseFee
		} else {
			head.BaseFee = new(big.Int).Set(g.Config.FeeConfig.MinBaseFee)
		}
	}
	statedb.Commit(false, false)
	// Commit newly generated states into disk if it's not empty.
	if root != types.EmptyRootHash {
		if err := triedb.Commit(root, true); err != nil {
			panic(fmt.Sprintf("unable to commit genesis block: %v", err))
		}
	}
	return types.NewBlock(head, nil, nil, nil, trie.NewStackTrie(nil))
}

// Commit writes the block and state of a genesis specification to the database.
// The block is committed as the canonical head block.
func (g *Genesis) Commit(db ethdb.Database, triedb *trie.Database) (*types.Block, error) {
	block := g.toBlock(db, triedb)
	if block.Number().Sign() != 0 {
		return nil, errors.New("can't commit genesis block with number > 0")
	}
	config := g.Config
	if config == nil {
		return nil, errGenesisNoConfig
	}
	if err := config.CheckConfigForkOrder(); err != nil {
		return nil, err
	}
	batch := db.NewBatch()
	rawdb.WriteBlock(batch, block)
	rawdb.WriteReceipts(batch, block.Hash(), block.NumberU64(), nil)
	rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())
	rawdb.WriteHeadBlockHash(batch, block.Hash())
	rawdb.WriteHeadHeaderHash(batch, block.Hash())
	rawdb.WriteChainConfig(batch, block.Hash(), config)
	if err := batch.Write(); err != nil {
		return nil, fmt.Errorf("failed to write genesis block: %w", err)
	}
	return block, nil
}

// MustCommit writes the genesis block and state to db, panicking on error.
// The block is committed as the canonical head block.
// Note the state changes will be committed in hash-based scheme, use Commit
// if path-scheme is preferred.
func (g *Genesis) MustCommit(db ethdb.Database) *types.Block {
	block, err := g.Commit(db, trie.NewDatabase(db))
	if err != nil {
		panic(err)
	}
	return block
}

func (g *Genesis) Verify() error {
	// Make sure genesis gas limit is consistent
	gasLimitConfig := g.Config.FeeConfig.GasLimit.Uint64()
	if gasLimitConfig != g.GasLimit {
		return fmt.Errorf(
			"gas limit in fee config (%d) does not match gas limit in header (%d)",
			gasLimitConfig,
			g.GasLimit,
		)
	}
	// Verify config
	if err := g.Config.Verify(); err != nil {
		return err
	}
	return nil
}

// GenesisBlockForTesting creates and writes a block in which addr has the given wei balance.
func GenesisBlockForTesting(db ethdb.Database, addr common.Address, balance *big.Int) *types.Block {
	g := Genesis{
		Config:  params.TestChainConfig,
		Alloc:   GenesisAlloc{addr: {Balance: balance}},
		BaseFee: big.NewInt(params.TestMaxBaseFee),
	}
	return g.MustCommit(db)
}

// ReadBlockByHash reads the block with the given hash from the database.
func ReadBlockByHash(db ethdb.Reader, hash common.Hash) *types.Block {
	blockNumber := rawdb.ReadHeaderNumber(db, hash)
	if blockNumber == nil {
		return nil
	}
	return rawdb.ReadBlock(db, hash, *blockNumber)
}
