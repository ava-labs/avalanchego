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
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap3"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/modules"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"

	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
	avalancheutils "github.com/ava-labs/avalanchego/utils"
	ethparams "github.com/ava-labs/libevm/params"
)

var (
	errNoGenesisChainConfig       = errors.New("no genesis chainConfig")
	errNoGenesisChainID           = errors.New("no genesis chainID")
	errNonZeroGenesisNumber       = errors.New("non-zero genesis number")
	errNonZeroGenesisGasUsed      = errors.New("non-zero genesis gasUsed")
	errNonZeroGenesisParentHash   = errors.New("non-zero genesis parentHash")
	errNonNilGenesisExcessBlobGas = errors.New("non-nil genesis excessBlobGas")
	errNonNilGenesisBlobGasUsed   = errors.New("non-nil genesis blobGasUsed")
)

// parseGenesis decodes the genesis bytes and populates the upgrade schedule.
func parseGenesis(ctx *snow.Context, b []byte) (*core.Genesis, error) {
	var g core.Genesis
	if err := json.Unmarshal(b, &g); err != nil {
		return nil, fmt.Errorf("unmarshalling genesis: %w", err)
	}

	// The genesis is committed as block 0 with no execution history, so the
	// fields that the Genesis type documents as for consensus tests only must
	// be left at their zero values. BaseFee is exempt: it is a valid (if inert)
	// genesis header field. See [flushGenesisState].
	switch {
	case g.Config == nil:
		return nil, errNoGenesisChainConfig
	case g.Config.ChainID == nil:
		return nil, errNoGenesisChainID
	case g.Number != 0:
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
	return &g, nil
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

// setupGenesis commits the genesis block and chain config to db if the genesis
// state is not already present. Otherwise it updates the stored chain config
// after verifying that the stored genesis hash matches, which may schedule
// upgrades that were not previously scheduled. It returns the genesis block.
func setupGenesis(
	db ethdb.Database,
	trieConfig *triedb.Config,
	genesis *core.Genesis,
) (*types.Block, error) {
	stored := rawdb.ReadCanonicalHash(db, 0)
	if (stored == common.Hash{}) {
		return initializeGenesis(db, trieConfig, genesis)
	}

	// If the chain is already initialized, the stored genesis hash must match.
	block, err := genesisToBlock(genesis)
	if err != nil {
		return nil, err
	}
	if hash := block.Hash(); hash != stored {
		return nil, &core.GenesisMismatchError{
			Stored: stored,
			New:    hash,
		}
	}

	// TODO(StephenButtolph): Consider checking compatibility of the chain
	// config against the last accepted block.
	rawdb.WriteChainConfig(db, stored, genesis.Config)
	return block, nil
}

// genesisToBlock builds the genesis block without persisting its state to db,
// for hash comparison. It mirrors coreth's core.Genesis.ToBlock, which is
// unavailable for libevm's Genesis because the block construction is
// Avalanche-specific. The state is flushed to a throwaway in-memory database.
func genesisToBlock(genesis *core.Genesis) (*types.Block, error) {
	return writeGenesisState(
		rawdb.NewMemoryDatabase(),
		triedb.HashDefaults,
		genesis,
	)
}

// initializeGenesis writes the genesis block, state, and chain config to db,
// returning the genesis block.
//
// It inlines [core.Genesis.Commit] (and the unexported core.Genesis.toBlock it
// builds on), kept in sync with graft/coreth, so that the trie database can be
// closed after the genesis state is flushed but before the canonical hash is
// written. Closing first guarantees that a crash can never leave the canonical
// hash on disk without the corresponding state, which matters for firewood: it
// persists the trie independently of the canonical hash and only flushes it
// durably on close. hashdb and pathdb flush the trie inline, so for them the
// ordering is merely a no-op safeguard.
func initializeGenesis(
	db ethdb.Database,
	trieConfig *triedb.Config,
	genesis *core.Genesis,
) (*types.Block, error) {
	block, err := writeGenesisState(db, trieConfig, genesis)
	if err != nil {
		return nil, err
	}

	b := db.NewBatch()
	rawdb.WriteBlock(b, block)
	rawdb.WriteReceipts(b, block.Hash(), block.NumberU64(), nil)
	rawdb.WriteCanonicalHash(b, block.Hash(), block.NumberU64())
	rawdb.WriteHeadBlockHash(b, block.Hash())
	rawdb.WriteHeadHeaderHash(b, block.Hash())
	rawdb.WriteChainConfig(b, block.Hash(), genesis.Config)
	if err := b.Write(); err != nil {
		return nil, fmt.Errorf("writing genesis block metadata: %w", err)
	}
	return block, nil
}

func writeGenesisState(
	db ethdb.Database,
	trieConfig *triedb.Config,
	genesis *core.Genesis,
) (_ *types.Block, retErr error) {
	tdb := triedb.NewDatabase(db, trieConfig)
	defer func() {
		retErr = errors.Join(retErr, tdb.Close())
	}()

	statedb, err := state.New(
		types.EmptyRootHash,
		extstate.NewDatabaseWithNodeDB(db, tdb),
		nil,
	)
	if err != nil {
		return nil, err
	}

	// Mark any stateful precompiles enabled at genesis as non-empty accounts.
	configExtra := corethparams.GetExtra(genesis.Config)
	applyPrecompileActivations(configExtra, genesis.Timestamp, statedb)

	for addr, account := range genesis.Alloc {
		statedb.SetBalance(addr, uint256.MustFromBig(account.Balance))
		statedb.SetCode(addr, account.Code)
		statedb.SetNonce(addr, account.Nonce)
		for key, value := range account.Storage {
			statedb.SetState(addr, key, value)
		}
	}

	root := statedb.IntermediateRoot(false)
	block := newGenesisBlock(genesis, root)
	triedbOpt := stateconf.WithTrieDBUpdatePayload(
		common.Hash{},
		block.Hash(),
	)
	if _, err := statedb.Commit(0, false, stateconf.WithTrieDBUpdateOpts(triedbOpt)); err != nil {
		return nil, fmt.Errorf("committing statedb: %w", err)
	}
	// Commit the generated state to the trie database if it is not empty.
	if root != types.EmptyRootHash {
		const logAsInfo = false
		if err := tdb.Commit(root, logAsInfo); err != nil {
			return nil, fmt.Errorf("committing triedb: %w", err)
		}
	}
	return block, nil
}

// applyPrecompileActivations marks every stateful precompile enabled at the
// genesis timestamp as a non-empty account, matching coreth's genesis state.
//
// It is a leaned-down version of coreth's core.ApplyPrecompileActivations: the
// C-Chain only ever configures the Warp precompile, whose Configure hook is a
// no-op, so this writes the nonce and code that make the precompile address
// callable but does not run any Configure hooks. A precompile that activates
// here with state-writing configuration would diverge from coreth.
func applyPrecompileActivations(c *extras.ChainConfig, timestamp uint64, statedb *state.StateDB) {
	for _, module := range modules.RegisteredModules() {
		for _, activatingConfig := range c.GetActivatingPrecompileConfigs(module.Address, nil, timestamp, c.PrecompileUpgrades) {
			// If this transition disables a precompile, deconfigure it.
			if activatingConfig.IsDisabled() {
				log.Info("Disabling precompile", "name", module.ConfigKey)
				statedb.SelfDestruct(module.Address)
				// Finalise the SelfDestruct immediately so the contract state is
				// wiped and can be re-configured within the same block.
				statedb.Finalise(true)
				continue
			}
			log.Info("Activating precompile", "name", module.ConfigKey)
			// Mark the precompile address as non-empty (nonce + code) so it is
			// not cleaned up when the statedb is finalized and can be called
			// from Solidity contracts.
			statedb.SetNonce(module.Address, 1)
			statedb.SetCode(module.Address, []byte{0x1})
		}
	}
}

func newGenesisBlock(genesis *core.Genesis, root common.Hash) *types.Block {
	header := &types.Header{
		ParentHash: common.Hash{},
		// UncleHash is set by [types.NewBlock].
		Coinbase: genesis.Coinbase,
		Root:     root,
		// TxHash is set by [types.NewBlock].
		// ReceiptHash is set by [types.NewBlock].
		Bloom:      types.Bloom{},
		Difficulty: genesis.Difficulty,
		Number:     new(big.Int),
		GasLimit:   genesis.GasLimit,
		GasUsed:    0,
		Time:       genesis.Timestamp,
		Extra:      genesis.ExtraData,
		MixDigest:  genesis.Mixhash,
		Nonce:      types.EncodeNonce(genesis.Nonce),
		// BaseFee, BlobGasUsed, ExcessBlobGas, and ParentBeaconRoot were all
		// added in network upgrades, so they are optionally configured below.
		// WithdrawalsHash is not serialized by the libevm hooks, so it is
		// always nil.
	}
	if header.Difficulty == nil {
		header.Difficulty = ethparams.GenesisDifficulty
	}
	if header.GasLimit == 0 {
		header.GasLimit = ethparams.GenesisGasLimit
	}

	configExtra := corethparams.GetExtra(genesis.Config)
	if configExtra.IsApricotPhase3(genesis.Timestamp) {
		header.BaseFee = genesis.BaseFee
		if header.BaseFee == nil {
			header.BaseFee = big.NewInt(ap3.InitialBaseFee)
		}
	}

	headerExtra := customtypes.GetHeaderExtra(header)
	if configExtra.IsEtna(genesis.Timestamp) {
		header.BlobGasUsed = new(uint64)
		header.ExcessBlobGas = new(uint64)
		header.ParentBeaconRoot = new(common.Hash)

		headerExtra.ExtDataGasUsed = new(big.Int)
		headerExtra.BlockGasCost = new(big.Int)
	}

	if configExtra.IsGranite(genesis.Timestamp) {
		headerExtra.TimeMilliseconds = avalancheutils.PointerTo(genesis.Timestamp * 1000)
		headerExtra.MinDelayExcess = avalancheutils.PointerTo(acp226.InitialDelayExcess)
	}

	return types.NewBlock(
		header,
		nil, // txs
		nil, // uncles
		nil, // receipts
		trie.NewStackTrie(nil),
	)
}
