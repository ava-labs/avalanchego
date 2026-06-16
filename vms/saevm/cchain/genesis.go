// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap3"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"

	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
	avalancheutils "github.com/ava-labs/avalanchego/utils"
	ethparams "github.com/ava-labs/libevm/params"
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
)

type genesis core.Genesis

// parseGenesis decodes the genesis bytes and populates the upgrade schedule.
func parseGenesis(ctx *snow.Context, b []byte) (*genesis, error) {
	var g core.Genesis
	if err := json.Unmarshal(b, &g); err != nil {
		return nil, fmt.Errorf("unmarshalling genesis: %w", err)
	}

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

	// The JSON only specifies the chain-specific configuration; the upgrade
	// schedule is configured by ctx.
	chainID := g.Config.ChainID
	u := &ctx.NetworkUpgrades
	g.Config = corethparams.WithExtra(
		&ethparams.ChainConfig{
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
			BerlinBlock:         big.NewInt(berlinBlock(chainID)),
			LondonBlock:         big.NewInt(londonBlock(chainID)),
			ShanghaiTime:        utils.TimeToNewUint64(u.DurangoTime),
			CancunTime:          utils.TimeToNewUint64(u.EtnaTime),
		},
		&extras.ChainConfig{
			NetworkUpgrades: extras.NetworkUpgrades{
				ApricotPhase1BlockTimestamp:     utils.TimeToNewUint64(u.ApricotPhase1Time),
				ApricotPhase2BlockTimestamp:     utils.TimeToNewUint64(u.ApricotPhase2Time),
				ApricotPhase3BlockTimestamp:     utils.TimeToNewUint64(u.ApricotPhase3Time),
				ApricotPhase4BlockTimestamp:     utils.TimeToNewUint64(u.ApricotPhase4Time),
				ApricotPhase5BlockTimestamp:     utils.TimeToNewUint64(u.ApricotPhase5Time),
				ApricotPhasePre6BlockTimestamp:  utils.TimeToNewUint64(u.ApricotPhasePre6Time),
				ApricotPhase6BlockTimestamp:     utils.TimeToNewUint64(u.ApricotPhase6Time),
				ApricotPhasePost6BlockTimestamp: utils.TimeToNewUint64(u.ApricotPhasePost6Time),
				BanffBlockTimestamp:             utils.TimeToNewUint64(u.BanffTime),
				CortinaBlockTimestamp:           utils.TimeToNewUint64(u.CortinaTime),
				DurangoBlockTimestamp:           utils.TimeToNewUint64(u.DurangoTime),
				EtnaTimestamp:                   utils.TimeToNewUint64(u.EtnaTime),
				FortunaTimestamp:                utils.TimeToNewUint64(u.FortunaTime),
				GraniteTimestamp:                utils.TimeToNewUint64(u.GraniteTime),
				HeliconTimestamp:                utils.TimeToNewUint64(u.HeliconTime),
			},
			AvalancheContext: extras.AvalancheContext{
				SnowCtx: ctx,
			},
			UpgradeConfig: extras.UpgradeConfig{
				PrecompileUpgrades: []extras.PrecompileUpgrade{
					{
						Config: warp.NewDefaultConfig(
							utils.TimeToNewUint64(u.DurangoTime),
						),
					},
				},
			},
		},
	)
	return (*genesis)(&g), nil
}

var (
	mainnetChainID = big.NewInt(43114)
	fujiChainID    = big.NewInt(43113)
)

func berlinBlock(chainID *big.Int) int64 {
	switch {
	case utils.BigEqual(chainID, mainnetChainID):
		return 1_640_340 // https://snowtrace.io/block/1640340?chainid=43114, AP2 activation block
	case utils.BigEqual(chainID, fujiChainID):
		return 184_985 // https://testnet.snowtrace.io/block/184985?chainid=43113, AP2 activation block
	default:
		return 0
	}
}

func londonBlock(chainID *big.Int) int64 {
	switch {
	case utils.BigEqual(chainID, mainnetChainID):
		return 3_308_552 // https://snowtrace.io/block/3308552?chainid=43114, AP3 activation block
	case utils.BigEqual(chainID, fujiChainID):
		return 805_078 // https://testnet.snowtrace.io/block/805078?chainid=43113, AP3 activation block
	default:
		return 0
	}
}

var (
	errNoStoredChainConfig = errors.New("no stored chainConfig")
	errNoHeadHeader        = errors.New("no head header")
)

// setup configures the database with genesis.
//
// It verifies that the genesis is compatible with any previously setup genesis
// state by checking the genesis block hash along with the rules used to execute
// the head block.
func (g *genesis) setup(db ethdb.Database, trieConfig *triedb.Config) (_ *types.Block, retErr error) {
	root, err := g.root()
	if err != nil {
		return nil, fmt.Errorf("getting state root: %w", err)
	}

	// We can't exit early here. Even if the genesis block is on disk, the
	// genesis state might not be.
	block := g.block(root)
	hash := block.Hash()
	if prev := rawdb.ReadCanonicalHash(db, genesisNumber); prev == (common.Hash{}) {
		if err := writeGenesisBlock(db, block, g.Config); err != nil {
			return nil, fmt.Errorf("writing block: %w", err)
		}
	} else if prev != hash {
		return nil, &core.GenesisMismatchError{
			Stored: prev,
			New:    hash,
		}
	}

	// If the rules change for the head block, it may have been executed
	// incorrectly.
	{
		prev := rawdb.ReadChainConfig(db, hash)
		if prev == nil {
			return nil, errNoStoredChainConfig
		}
		head := rawdb.ReadHeadHeader(db)
		if head == nil {
			return nil, errNoHeadHeader
		}
		height, timestamp := head.Number.Uint64(), head.Time
		if err := prev.CheckCompatible(g.Config, height, timestamp); err != nil {
			return nil, fmt.Errorf("incompatible chain config: %w", err)
		}
	}
	rawdb.WriteChainConfig(db, hash, g.Config)

	tdb := triedb.NewDatabase(db, trieConfig)
	defer func() {
		retErr = errors.Join(retErr, tdb.Close())
	}()

	// Because some trie implementations prune old state, we need to defer to
	// the trie to determine if the genesis was previously initialized.
	if !tdb.Initialized(block.Root()) {
		if _, err := g.writeState(db, tdb); err != nil {
			return nil, fmt.Errorf("writing genesis state: %w", err)
		}
	}
	return block, nil
}

func (g *genesis) root() (_ common.Hash, retErr error) {
	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(db, triedb.HashDefaults)
	defer func() {
		retErr = errors.Join(retErr, tdb.Close())
	}()
	return g.writeState(db, tdb)
}

func writeGenesisBlock(db ethdb.Database, block *types.Block, config *ethparams.ChainConfig) error {
	b := db.NewBatch()
	hash := block.Hash()

	rawdb.WriteBlock(b, block)
	rawdb.WriteReceipts(b, hash, genesisNumber, nil)
	rawdb.WriteCanonicalHash(b, hash, genesisNumber)
	rawdb.WriteFinalizedBlockHash(b, hash)
	rawdb.WriteHeadBlockHash(b, hash)
	rawdb.WriteHeadHeaderHash(b, hash)
	rawdb.WriteChainConfig(b, hash, config)
	return b.Write()
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

	for addr, account := range g.Alloc {
		statedb.SetBalance(addr, uint256.MustFromBig(account.Balance))
		statedb.SetCode(addr, account.Code)
		statedb.SetNonce(addr, account.Nonce)
		for key, value := range account.Storage {
			statedb.SetState(addr, key, value)
		}
	}
	// Precompile upgrades happen at the activation of the network upgrade. If
	// the genesis timestamp is already after the Warp activation, then the
	// state needs to reflect that or the precompile would never be marked as
	// active.
	//
	// When a precompile is activated, its account is marked as non-empty by
	// setting the nonce and code so it is not pruned as an empty account during
	// state finalization (EIP-161) and so it appears as a contract to EVM code
	// introspection (e.g. EXTCODESIZE/EXTCODEHASH).
	const (
		precompileNonce = 1
		precompileCode  = "\x01"
	)
	if c := corethparams.GetExtra(g.Config); c.IsDurango(g.Timestamp) {
		statedb.SetNonce(warp.ContractAddress, precompileNonce)
		statedb.SetCode(warp.ContractAddress, []byte(precompileCode))
	}

	const deleteEmptyObjects = true
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

func (g *genesis) block(root common.Hash) *types.Block {
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
		h.GasLimit = ethparams.GenesisGasLimit
	}

	c := corethparams.GetExtra(g.Config)
	if c.IsApricotPhase3(g.Timestamp) {
		h.BaseFee = g.BaseFee
		if h.BaseFee == nil {
			h.BaseFee = big.NewInt(ap3.InitialBaseFee)
		}
	}

	headerExtra := customtypes.GetHeaderExtra(h)
	if c.IsEtna(g.Timestamp) {
		h.BlobGasUsed = new(uint64)
		h.ExcessBlobGas = new(uint64)
		h.ParentBeaconRoot = new(common.Hash)

		headerExtra.ExtDataGasUsed = new(big.Int)
		headerExtra.BlockGasCost = new(big.Int)
	}

	if c.IsGranite(g.Timestamp) {
		headerExtra.TimeMilliseconds = avalancheutils.PointerTo(g.Timestamp * 1000)
		headerExtra.MinDelayExcess = avalancheutils.PointerTo(acp226.InitialDelayExcess)
	}

	return types.NewBlock(
		h,
		nil, // txs
		nil, // uncles
		nil, // receipts
		trie.NewStackTrie(nil),
	)
}
