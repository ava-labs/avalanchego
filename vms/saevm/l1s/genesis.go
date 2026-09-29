// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"

	l1params "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
)

const genesisNumber = 0

var (
	errNoGenesisChainConfig       = errors.New("no genesis chainConfig")
	errNoGenesisChainID           = errors.New("no genesis chainID")
	errNonZeroGenesisNumber       = errors.New("non-zero genesis number")
	errNonZeroGenesisGasUsed      = errors.New("non-zero genesis gasUsed")
	errNonZeroGenesisParentHash   = errors.New("non-zero genesis parentHash")
	errNonNilGenesisExcessBlobGas = errors.New("non-nil genesis excessBlobGas")
	errNonNilGenesisBlobGasUsed   = errors.New("non-nil genesis blobGasUsed")
	errGasLimitMismatch           = errors.New("gas limit mismatch")
)

type genesis core.Genesis

// parseGenesis decodes the genesis bytes and populates the upgrade schedule.
//
// airdropData is the content of the airdrop file referenced by the genesis
// AirdropHash, if any.
func parseGenesis(ctx *snow.Context, genesisBytes, upgradeBytes, airdropData []byte) (*genesis, error) {
	var g core.Genesis
	if err := json.Unmarshal(genesisBytes, &g); err != nil {
		return nil, fmt.Errorf("unmarshalling genesis: %w", err)
	}

	var upgradeConfig extras.UpgradeConfig
	if len(upgradeBytes) > 0 {
		if err := json.Unmarshal(upgradeBytes, &upgradeConfig); err != nil {
			return nil, fmt.Errorf("unmarshalling upgrade: %w", err)
		}
	}

	g.AirdropData = airdropData

	// Almost all of the fields in [core.Genesis] that are marked as testing
	// only are explicitly disallowed. The only such field that is allowed to be
	// configured is [core.Genesis.BaseFee], as SAE initializes the gas price
	// to the last synchronous block's BaseFee.
	switch {
	case g.Config == nil:
		return nil, errNoGenesisChainConfig
	case g.Config.ChainID == nil:
		return nil, errNoGenesisChainID
	case g.Number != genesisNumber:
		return nil, fmt.Errorf("%w: %d", errNonZeroGenesisNumber, g.Number)
	case g.GasUsed != 0:
		return nil, fmt.Errorf("%w: %d", errNonZeroGenesisGasUsed, g.GasUsed)
	case g.ParentHash != (common.Hash{}):
		return nil, fmt.Errorf("%w: %s", errNonZeroGenesisParentHash, g.ParentHash)
	case g.ExcessBlobGas != nil:
		return nil, fmt.Errorf("%w: %d", errNonNilGenesisExcessBlobGas, *g.ExcessBlobGas)
	case g.BlobGasUsed != nil:
		return nil, fmt.Errorf("%w: %d", errNonNilGenesisBlobGasUsed, *g.BlobGasUsed)
	}

	extras, err := newExtras(ctx, g.Config, upgradeConfig)
	if err != nil {
		return nil, err
	}
	if gasLimit := extras.FeeConfig.GasLimit.Uint64(); gasLimit != g.GasLimit {
		return nil, fmt.Errorf("%w: fee config %d vs genesis %d", errGasLimitMismatch, gasLimit, g.GasLimit)
	}

	chainID := g.Config.ChainID
	g.Config = l1params.WithExtra(
		&params.ChainConfig{
			ChainID:             chainID,
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        big.NewInt(0),
			DAOForkSupport:      true,
			EIP150Block:         big.NewInt(0),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
			IstanbulBlock:       big.NewInt(0),
			MuirGlacierBlock:    big.NewInt(0),
			BerlinBlock:         big.NewInt(0),
			LondonBlock:         big.NewInt(0),
			ShanghaiTime:        extras.DurangoTimestamp,
			CancunTime:          extras.EtnaTimestamp,
		},
		extras,
	)
	return (*genesis)(&g), nil
}

func newExtras(ctx *snow.Context, cfg *params.ChainConfig, upgradeConfig extras.UpgradeConfig) (*extras.ChainConfig, error) {
	provided := l1params.GetExtra(cfg)
	cpy := *provided
	if cpy.FeeConfig == commontype.EmptyFeeConfig {
		cpy.FeeConfig = l1params.DefaultFeeConfig
	}

	cpy.NetworkUpgrades = newNetworkUpgrades(ctx, upgradeConfig)
	cpy.UpgradeConfig = upgradeConfig
	cpy.AvalancheContext = extras.AvalancheContext{
		SnowCtx: ctx,
	}

	if err := cpy.Verify(); err != nil {
		return nil, err
	}

	return &cpy, nil
}

func newNetworkUpgrades(ctx *snow.Context, upgradeConfig extras.UpgradeConfig) extras.NetworkUpgrades {
	u := &ctx.NetworkUpgrades
	upgrades := extras.NetworkUpgrades{
		SubnetEVMTimestamp: new(uint64(0)),
		DurangoTimestamp:   utils.TimeToNewUint64(u.DurangoTime),
		EtnaTimestamp:      utils.TimeToNewUint64(u.EtnaTime),
		FortunaTimestamp:   nil,
		GraniteTimestamp:   utils.TimeToNewUint64(u.GraniteTime),
		HeliconTimestamp:   utils.TimeToNewUint64(u.HeliconTime),
	}

	overrides := extras.NetworkUpgrades{}
	if upgradeConfig.NetworkUpgradeOverrides != nil {
		overrides = *upgradeConfig.NetworkUpgradeOverrides
	}

	if overrides.SubnetEVMTimestamp != nil {
		upgrades.SubnetEVMTimestamp = overrides.SubnetEVMTimestamp
	}
	if overrides.DurangoTimestamp != nil {
		upgrades.DurangoTimestamp = overrides.DurangoTimestamp
	}
	if overrides.EtnaTimestamp != nil {
		upgrades.EtnaTimestamp = overrides.EtnaTimestamp
	}
	if overrides.FortunaTimestamp != nil {
		upgrades.FortunaTimestamp = overrides.FortunaTimestamp
	}
	if overrides.GraniteTimestamp != nil {
		upgrades.GraniteTimestamp = overrides.GraniteTimestamp
	}
	if overrides.HeliconTimestamp != nil {
		upgrades.HeliconTimestamp = overrides.HeliconTimestamp
	}
	return upgrades
}

var errNoHeadHeader = errors.New("no head header")

// verifyAndWriteBlock verifies that the genesis is compatible with any
// previously stored genesis state by checking the genesis block hash along with
// the rules used to execute the head block.
//
// Once the chain is ready to be initialized, one must call
// [genesis.setupTrieDB] to ensure the genesis state is available.
func (g *genesis) verifyAndWriteBlock(db ethdb.Database) error {
	block, err := g.block()
	if err != nil {
		return fmt.Errorf("constructing genesis block: %w", err)
	}

	hash := block.Hash()
	if prev := rawdb.ReadCanonicalHash(db, genesisNumber); prev == (common.Hash{}) {
		if err := writeGenesisBlock(db, block, g.Config); err != nil {
			return fmt.Errorf("writing block: %w", err)
		}
	} else if prev != hash {
		return &core.GenesisMismatchError{
			Stored: prev,
			New:    hash,
		}
	}

	// If the rules change for the head block, it may have been executed
	// incorrectly.  The upgrade config is stored separately from the chain
	// config.
	var prevUpgrades extras.UpgradeConfig
	prev, err := customrawdb.ReadChainConfig(db, hash, &prevUpgrades)
	if err != nil {
		return fmt.Errorf("reading stored chain config: %w", err)
	}
	l1params.GetExtra(prev).UpgradeConfig = prevUpgrades

	head := rawdb.ReadHeadHeader(db)
	if head == nil {
		return errNoHeadHeader
	}
	height, timestamp := head.Number.Uint64(), head.Time
	// TODO(JonathanOppenheimer): subnet-evm exposes a `skip-upgrade-check`
	// config that bypasses this compatibility check; decide whether L1s need it.
	if err := prev.CheckCompatible(g.Config, height, timestamp); err != nil {
		return fmt.Errorf("incompatible chain config: %w", err)
	}

	// We will be executing new blocks based on the new chain config, so we
	// need to keep it up-to-date in the database for the next restart.
	b := db.NewBatch()
	if err := customrawdb.WriteChainConfig(b, hash, g.Config, l1params.GetExtra(g.Config).UpgradeConfig); err != nil {
		return fmt.Errorf("writing chain config: %w", err)
	}
	return b.Write()
}

func writeGenesisBlock(db ethdb.Database, block *types.Block, config *params.ChainConfig) error {
	b := db.NewBatch()
	hash := block.Hash()
	upgradeCfg := l1params.GetExtra(config).UpgradeConfig

	rawdb.WriteBlock(b, block)
	rawdb.WriteReceipts(b, hash, genesisNumber, nil)
	rawdb.WriteCanonicalHash(b, hash, genesisNumber)
	rawdb.WriteFinalizedBlockHash(b, hash)
	rawdb.WriteHeadBlockHash(b, hash)
	rawdb.WriteHeadHeaderHash(b, hash)
	rawdb.WriteHeadFastBlockHash(b, hash)
	if err := customrawdb.WriteChainConfig(b, hash, config, upgradeCfg); err != nil {
		return fmt.Errorf("writing chain config: %w", err)
	}
	return b.Write()
}

// block constructs the genesis block. The header MUST be identical to the one
// produced by subnet-evm for the same genesis so that existing L1s retain
// their genesis hash.
func (g *genesis) block() (*types.Block, error) {
	root, err := g.root()
	if err != nil {
		return nil, fmt.Errorf("computing state root: %w", err)
	}

	h := &types.Header{
		ParentHash: common.Hash{},
		// UncleHash is set by [types.NewBlock].
		Coinbase: g.Coinbase,
		Root:     root,
		// TxHash is set by [types.NewBlock].
		// ReceiptHash is set by [types.NewBlock].
		Bloom:      types.Bloom{},
		Difficulty: g.Difficulty,
		Number:     new(big.Int),
		GasLimit:   g.GasLimit,
		GasUsed:    0,
		Time:       g.Timestamp,
		Extra:      g.ExtraData,
		MixDigest:  g.Mixhash,
		Nonce:      types.EncodeNonce(g.Nonce),
		// BaseFee, BlobGasUsed, ExcessBlobGas, and ParentBeaconRoot were all
		// added in network upgrades, so they are optionally configured below.
		// WithdrawalsHash is not serialized by the libevm hooks, so it is
		// always nil.
	}
	if h.GasLimit == 0 {
		h.GasLimit = params.GenesisGasLimit
	}

	c := l1params.GetExtra(g.Config)
	if c.IsSubnetEVM(g.Timestamp) { // Includes London
		h.BaseFee = g.BaseFee
		if h.BaseFee == nil {
			h.BaseFee = new(big.Int).Set(c.FeeConfig.MinBaseFee)
		}
	}

	headerExtra := customtypes.GetHeaderExtra(h)
	if c.IsEtna(g.Timestamp) { // Also called Cancun
		h.BlobGasUsed = new(uint64)
		h.ExcessBlobGas = new(uint64)
		h.ParentBeaconRoot = new(common.Hash)

		headerExtra.BlockGasCost = new(big.Int)
	}

	if c.IsGranite(g.Timestamp) {
		headerExtra.TimeMilliseconds = new(g.Timestamp * 1000)

		minDelayExcess := acp226.InitialDelayExcess
		if c.InitialMinDelayMS != 0 {
			minDelayExcess = acp226.DesiredDelayExcess(c.InitialMinDelayMS)
		}
		headerExtra.MinDelayExcess = new(minDelayExcess)
	}

	// TODO: Add SAE required fields if active at genesis.

	return types.NewBlock(
		h,
		nil, // txs
		nil, // uncles
		nil, // receipts
		trie.NewStackTrie(nil),
	), nil
}

func (g *genesis) root() (_ common.Hash, retErr error) {
	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(db, triedb.HashDefaults)
	defer func() {
		retErr = errors.Join(retErr, tdb.Close())
	}()
	return g.writeState(db, tdb)
}

// setupTrieDB commits the genesis allocation to the state database if
// it is not already present.
func (g *genesis) setupTrieDB(db ethdb.Database, trieConfig *triedb.Config) (retErr error) {
	root, err := g.root()
	if err != nil {
		return fmt.Errorf("computing genesis root: %w", err)
	}

	tdb := triedb.NewDatabase(db, trieConfig)
	defer func() {
		retErr = errors.Join(retErr, tdb.Close())
	}()

	if tdb.Initialized(root) {
		return nil
	}

	_, err = g.writeState(db, tdb)
	return err
}

// writeState commits the genesis allocation to the state database and returns
// the state root.
func (g *genesis) writeState(db ethdb.Database, tdb *triedb.Database) (common.Hash, error) {
	statedb, err := state.New(
		types.EmptyRootHash,
		state.NewDatabaseWithNodeDB(db, tdb),
		nil,
	)
	if err != nil {
		return common.Hash{}, err
	}

	airdrop, err := g.airdrop()
	if err != nil {
		return common.Hash{}, err
	}
	amount := uint256.MustFromBig(g.AirdropAmount)
	for _, a := range airdrop {
		statedb.SetBalance(a.Address, amount)
	}

	// TODO: Register precompiles. See [core.ApplyPrecompileActivations].

	// The explicit allocation is applied last so that it takes precedence
	// over the airdrop.
	for addr, account := range g.Alloc {
		statedb.SetBalance(addr, uint256.MustFromBig(account.Balance))
		statedb.SetCode(addr, account.Code)
		statedb.SetNonce(addr, account.Nonce)
		for key, value := range account.Storage {
			statedb.SetState(addr, key, value)
		}
	}

	const deleteEmptyObjects = false
	root, err := statedb.Commit(genesisNumber, deleteEmptyObjects)
	if err != nil {
		return common.Hash{}, fmt.Errorf("committing statedb: %w", err)
	}
	const logAsInfo = false
	if err := tdb.Commit(root, logAsInfo); err != nil {
		return common.Hash{}, fmt.Errorf("committing triedb: %w", err)
	}
	return root, nil
}

var (
	errAirdropHashMismatch = errors.New("airdrop hash mismatch")
	errNoAirdropAmount     = errors.New("no airdrop amount")
)

// airdrop verifies the airdrop data against the configured hash and decodes
// the airdrop addresses.
func (g *genesis) airdrop() ([]*core.Airdrop, error) {
	if len(g.AirdropData) == 0 {
		if g.AirdropHash != (common.Hash{}) {
			return nil, fmt.Errorf("%w: missing expected airdrop data", errAirdropHashMismatch)
		}
		return nil, nil
	}

	if h := common.BytesToHash(crypto.Keccak256(g.AirdropData)); h != g.AirdropHash {
		return nil, fmt.Errorf("%w: expected %s but got %s", errAirdropHashMismatch, g.AirdropHash, h)
	}
	var airdrop []*core.Airdrop
	if err := json.Unmarshal(g.AirdropData, &airdrop); err != nil {
		return nil, fmt.Errorf("unmarshalling airdrop: %w", err)
	}

	if g.AirdropAmount == nil {
		return nil, errNoAirdropAmount
	}
	return airdrop, nil
}
