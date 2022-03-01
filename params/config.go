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
// Copyright 2016 The go-ethereum Authors
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

package params

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/subnet-evm/precompile"
	"github.com/ava-labs/subnet-evm/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

var (
	errNonGenesisForkByHeight = errors.New("subnet-evm only supports forking by height at the genesis block")

	SubnetEVMChainID = big.NewInt(43214)

	// For legacy tests
	MinGasPrice        int64 = 225_000_000_000
	TestInitialBaseFee int64 = 225_000_000_000
	TestMinBaseFee           = big.NewInt(75_000_000_000)
	TestMaxBaseFee           = big.NewInt(225_000_000_000)

	ExtraDataSize        = 80
	RollupWindow  uint64 = 10

	DefaultFeeConfig = &FeeConfig{
		GasLimit:        big.NewInt(8_000_000),
		TargetBlockRate: 2, // in seconds

		MinBaseFee:               big.NewInt(25_000_000_000),
		TargetGas:                big.NewInt(15_000_000),
		BaseFeeChangeDenominator: big.NewInt(36),

		MinBlockGasCost:  big.NewInt(0),
		MaxBlockGasCost:  big.NewInt(1_000_000),
		BlockGasCostStep: big.NewInt(200_000),
	}
)

var (
	// AvalancheMainnetChainConfig is the configuration for Avalanche Main Network
	SubnetEVMDefaultChainConfig = &ChainConfig{
		ChainID:             SubnetEVMChainID,
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP150Hash:          common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		SubnetEVMTimestamp:  big.NewInt(0),
		FeeConfig:           DefaultFeeConfig,
		AllowFeeRecipients:  false,
	}

	AllowListConfig = &ChainConfig{
		ChainID:             big.NewInt(43112),
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP150Hash:          common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		SubnetEVMTimestamp:  big.NewInt(0),
		FeeConfig:           DefaultFeeConfig,
		AllowFeeRecipients:  false,
		AllowListConfig: &precompile.AllowListConfig{
			BlockTimestamp: big.NewInt(0),
			AllowListAdmins: []common.Address{
				common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
				common.HexToAddress("0xF60C45c607D0f41687c94C314d300f483661E13a"),
			},
		},
	}

	TestChainConfig        = &ChainConfig{big.NewInt(1), big.NewInt(0), big.NewInt(0), common.Hash{}, big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0), DefaultFeeConfig, false, nil}
	TestPreSubnetEVMConfig = &ChainConfig{big.NewInt(1), big.NewInt(0), big.NewInt(0), common.Hash{}, big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0), nil, DefaultFeeConfig, false, nil}
)

// ChainConfig is the core config which determines the blockchain settings.
//
// ChainConfig is stored in the database on a per block basis. This means
// that any network, identified by its genesis block, can have its own
// set of configuration options.
type ChainConfig struct {
	ChainID *big.Int `json:"chainId"` // chainId identifies the current chain and is used for replay protection

	HomesteadBlock *big.Int `json:"homesteadBlock,omitempty"` // Homestead switch block (nil = no fork, 0 = already homestead)

	// EIP150 implements the Gas price changes (https://github.com/ethereum/EIPs/issues/150)
	EIP150Block *big.Int    `json:"eip150Block,omitempty"` // EIP150 HF block (nil = no fork)
	EIP150Hash  common.Hash `json:"eip150Hash,omitempty"`  // EIP150 HF hash (needed for header only clients as only gas pricing changed)

	EIP155Block *big.Int `json:"eip155Block,omitempty"` // EIP155 HF block
	EIP158Block *big.Int `json:"eip158Block,omitempty"` // EIP158 HF block

	ByzantiumBlock      *big.Int `json:"byzantiumBlock,omitempty"`      // Byzantium switch block (nil = no fork, 0 = already on byzantium)
	ConstantinopleBlock *big.Int `json:"constantinopleBlock,omitempty"` // Constantinople switch block (nil = no fork, 0 = already activated)
	PetersburgBlock     *big.Int `json:"petersburgBlock,omitempty"`     // Petersburg switch block (nil = same as Constantinople)
	IstanbulBlock       *big.Int `json:"istanbulBlock,omitempty"`       // Istanbul switch block (nil = no fork, 0 = already on istanbul)
	MuirGlacierBlock    *big.Int `json:"muirGlacierBlock,omitempty"`    // Eip-2384 (bomb delay) switch block (nil = no fork, 0 = already activated)

	SubnetEVMTimestamp *big.Int `json:"subnetEVMTimestamp,omitempty"` // A placeholder for the latest avalanche forks (nil = no fork, 0 = already activated)

	FeeConfig          *FeeConfig `json:"feeConfig,omitempty"`
	AllowFeeRecipients bool       `json:"allowFeeRecipients,omitempty"` // Allows fees to be collected by block builders.

	AllowListConfig *precompile.AllowListConfig `json:"allowListConfig,omitempty"` // Config for the allow list precompile
}

type FeeConfig struct {
	GasLimit        *big.Int `json:"gasLimit,omitempty"`
	TargetBlockRate uint64   `json:"targetBlockRate,omitempty"`

	MinBaseFee               *big.Int `json:"minBaseFee,omitempty"`
	TargetGas                *big.Int `json:"targetGas,omitempty"`
	BaseFeeChangeDenominator *big.Int `json:"baseFeeChangeDenominator,omitempty"`

	MinBlockGasCost  *big.Int `json:"minBlockGasCost,omitempty"`
	MaxBlockGasCost  *big.Int `json:"maxBlockGasCost,omitempty"`
	BlockGasCostStep *big.Int `json:"blockGasCostStep,omitempty"`
}

// String implements the fmt.Stringer interface.
func (c *ChainConfig) String() string {
	feeBytes, err := json.Marshal(c.FeeConfig)
	if err != nil {
		feeBytes = []byte("cannot unmarshal FeeConfig")
	}
	return fmt.Sprintf("{ChainID: %v Homestead: %v EIP150: %v EIP155: %v EIP158: %v Byzantium: %v Constantinople: %v Petersburg: %v Istanbul: %v, Muir Glacier: %v, Subnet EVM: %v, FeeConfig: %v, AllowFeeRecipients: %v, Engine: Dummy Consensus Engine}",
		c.ChainID,
		c.HomesteadBlock,
		c.EIP150Block,
		c.EIP155Block,
		c.EIP158Block,
		c.ByzantiumBlock,
		c.ConstantinopleBlock,
		c.PetersburgBlock,
		c.IstanbulBlock,
		c.MuirGlacierBlock,
		c.SubnetEVMTimestamp,
		string(feeBytes),
		c.AllowFeeRecipients,
	)
}

// IsHomestead returns whether num is either equal to the homestead block or greater.
func (c *ChainConfig) IsHomestead(num *big.Int) bool {
	return utils.IsForked(c.HomesteadBlock, num)
}

// IsEIP150 returns whether num is either equal to the EIP150 fork block or greater.
func (c *ChainConfig) IsEIP150(num *big.Int) bool {
	return utils.IsForked(c.EIP150Block, num)
}

// IsEIP155 returns whether num is either equal to the EIP155 fork block or greater.
func (c *ChainConfig) IsEIP155(num *big.Int) bool {
	return utils.IsForked(c.EIP155Block, num)
}

// IsEIP158 returns whether num is either equal to the EIP158 fork block or greater.
func (c *ChainConfig) IsEIP158(num *big.Int) bool {
	return utils.IsForked(c.EIP158Block, num)
}

// IsByzantium returns whether num is either equal to the Byzantium fork block or greater.
func (c *ChainConfig) IsByzantium(num *big.Int) bool {
	return utils.IsForked(c.ByzantiumBlock, num)
}

// IsConstantinople returns whether num is either equal to the Constantinople fork block or greater.
func (c *ChainConfig) IsConstantinople(num *big.Int) bool {
	return utils.IsForked(c.ConstantinopleBlock, num)
}

// IsMuirGlacier returns whether num is either equal to the Muir Glacier (EIP-2384) fork block or greater.
func (c *ChainConfig) IsMuirGlacier(num *big.Int) bool {
	return utils.IsForked(c.MuirGlacierBlock, num)
}

// IsPetersburg returns whether num is either
// - equal to or greater than the PetersburgBlock fork block,
// - OR is nil, and Constantinople is active
func (c *ChainConfig) IsPetersburg(num *big.Int) bool {
	return utils.IsForked(c.PetersburgBlock, num) || c.PetersburgBlock == nil && utils.IsForked(c.ConstantinopleBlock, num)
}

// IsIstanbul returns whether num is either equal to the Istanbul fork block or greater.
func (c *ChainConfig) IsIstanbul(num *big.Int) bool {
	return utils.IsForked(c.IstanbulBlock, num)
}

// IsSubnetEVM returns whether [blockTimestamp] is either equal to the SubnetEVM fork block timestamp or greater.
func (c *ChainConfig) IsSubnetEVM(blockTimestamp *big.Int) bool {
	return utils.IsForked(c.SubnetEVMTimestamp, blockTimestamp)
}

// IsAllowList returns whether [blockTimestamp] is either equal to the AllowList fork block timestamp or greater.
func (c *ChainConfig) IsAllowList(blockTimestamp *big.Int) bool {
	if c.AllowListConfig == nil {
		return false
	}
	return utils.IsForked(c.AllowListConfig.Timestamp(), blockTimestamp)
}

// GetFeeConfig returns the *FeeConfig if it exists.
func (c *ChainConfig) GetFeeConfig() *FeeConfig {
	if c.FeeConfig == nil {
		return DefaultFeeConfig
	}
	return c.FeeConfig
}

// CheckCompatible checks whether scheduled fork transitions have been imported
// with a mismatching chain configuration.
func (c *ChainConfig) CheckCompatible(newcfg *ChainConfig, height uint64, timestamp uint64) *ConfigCompatError {
	bhead := new(big.Int).SetUint64(height)
	bheadTimestamp := new(big.Int).SetUint64(timestamp)

	// Iterate checkCompatible to find the lowest conflict.
	var lasterr *ConfigCompatError
	for {
		err := c.checkCompatible(newcfg, bhead, bheadTimestamp)
		if err == nil || (lasterr != nil && err.RewindTo == lasterr.RewindTo) {
			break
		}
		lasterr = err
		bhead.SetUint64(err.RewindTo)
	}
	return lasterr
}

// CheckConfigForkOrder checks that we don't "skip" any forks, geth isn't pluggable enough
// to guarantee that forks can be implemented in a different order than on official networks
func (c *ChainConfig) CheckConfigForkOrder() error {
	type fork struct {
		name     string
		block    *big.Int
		optional bool // if true, the fork may be nil and next fork is still allowed
	}
	var lastFork fork
	for _, cur := range []fork{
		{name: "homesteadBlock", block: c.HomesteadBlock},
		{name: "eip150Block", block: c.EIP150Block},
		{name: "eip155Block", block: c.EIP155Block},
		{name: "eip158Block", block: c.EIP158Block},
		{name: "byzantiumBlock", block: c.ByzantiumBlock},
		{name: "constantinopleBlock", block: c.ConstantinopleBlock},
		{name: "petersburgBlock", block: c.PetersburgBlock},
		{name: "istanbulBlock", block: c.IstanbulBlock},
		{name: "muirGlacierBlock", block: c.MuirGlacierBlock, optional: true},
	} {
		if cur.block != nil && common.Big0.Cmp(cur.block) != 0 {
			return errNonGenesisForkByHeight
		}
		if lastFork.name != "" {
			// Next one must be higher number
			if lastFork.block == nil && cur.block != nil {
				return fmt.Errorf("unsupported fork ordering: %v not enabled, but %v enabled at %v",
					lastFork.name, cur.name, cur.block)
			}
			if lastFork.block != nil && cur.block != nil {
				if lastFork.block.Cmp(cur.block) > 0 {
					return fmt.Errorf("unsupported fork ordering: %v enabled at %v, but %v enabled at %v",
						lastFork.name, lastFork.block, cur.name, cur.block)
				}
			}
		}
		// If it was optional and not set, then ignore it
		if !cur.optional || cur.block != nil {
			lastFork = cur
		}
	}

	// Note: In Avalanche, hard forks must take place via block timestamps instead
	// of block numbers since blocks are produced asynchronously. Therefore, we do not
	// check that the block timestamps in the same way as for
	// the block number forks since it would not be a meaningful comparison.
	// Instead, we check only that Phases are enabled in order.
	// Note: we do not add the optional stateful precompile configs in here because they are optional
	// and independent, such that the ordering they are enabled does not impact the correctness of the
	// chain config.
	lastFork = fork{}
	for _, cur := range []fork{
		{name: "subnetEVMTimestamp", block: c.SubnetEVMTimestamp},
	} {
		if lastFork.name != "" {
			// Next one must be higher number
			if lastFork.block == nil && cur.block != nil {
				return fmt.Errorf("unsupported fork ordering: %v not enabled, but %v enabled at %v",
					lastFork.name, cur.name, cur.block)
			}
			if lastFork.block != nil && cur.block != nil {
				if lastFork.block.Cmp(cur.block) > 0 {
					return fmt.Errorf("unsupported fork ordering: %v enabled at %v, but %v enabled at %v",
						lastFork.name, lastFork.block, cur.name, cur.block)
				}
			}
		}
		// If it was optional and not set, then ignore it
		if !cur.optional || cur.block != nil {
			lastFork = cur
		}
	}
	return nil
}

func matchNil(a, b interface{}) (match, n bool) {
	switch {
	case a == nil && b == nil:
		return true, true
	case a != nil && b != nil:
		return true, false
	default:
		return false, false
	}
}

func (c *ChainConfig) checkCompatible(newcfg *ChainConfig, headHeight *big.Int, headTimestamp *big.Int) *ConfigCompatError {
	if isForkIncompatible(c.HomesteadBlock, newcfg.HomesteadBlock, headHeight) {
		return newCompatError("Homestead fork block", c.HomesteadBlock, newcfg.HomesteadBlock)
	}
	if isForkIncompatible(c.EIP150Block, newcfg.EIP150Block, headHeight) {
		return newCompatError("EIP150 fork block", c.EIP150Block, newcfg.EIP150Block)
	}
	if isForkIncompatible(c.EIP155Block, newcfg.EIP155Block, headHeight) {
		return newCompatError("EIP155 fork block", c.EIP155Block, newcfg.EIP155Block)
	}
	if isForkIncompatible(c.EIP158Block, newcfg.EIP158Block, headHeight) {
		return newCompatError("EIP158 fork block", c.EIP158Block, newcfg.EIP158Block)
	}
	if c.IsEIP158(headHeight) && !configNumEqual(c.ChainID, newcfg.ChainID) {
		return newCompatError("EIP158 chain ID", c.EIP158Block, newcfg.EIP158Block)
	}
	if isForkIncompatible(c.ByzantiumBlock, newcfg.ByzantiumBlock, headHeight) {
		return newCompatError("Byzantium fork block", c.ByzantiumBlock, newcfg.ByzantiumBlock)
	}
	if isForkIncompatible(c.ConstantinopleBlock, newcfg.ConstantinopleBlock, headHeight) {
		return newCompatError("Constantinople fork block", c.ConstantinopleBlock, newcfg.ConstantinopleBlock)
	}
	if isForkIncompatible(c.PetersburgBlock, newcfg.PetersburgBlock, headHeight) {
		// the only case where we allow Petersburg to be set in the past is if it is equal to Constantinople
		// mainly to satisfy fork ordering requirements which state that Petersburg fork be set if Constantinople fork is set
		if isForkIncompatible(c.ConstantinopleBlock, newcfg.PetersburgBlock, headHeight) {
			return newCompatError("Petersburg fork block", c.PetersburgBlock, newcfg.PetersburgBlock)
		}
	}
	if isForkIncompatible(c.IstanbulBlock, newcfg.IstanbulBlock, headHeight) {
		return newCompatError("Istanbul fork block", c.IstanbulBlock, newcfg.IstanbulBlock)
	}
	if isForkIncompatible(c.MuirGlacierBlock, newcfg.MuirGlacierBlock, headHeight) {
		return newCompatError("Muir Glacier fork block", c.MuirGlacierBlock, newcfg.MuirGlacierBlock)
	}

	// Check subnet-evm specific activations
	if isForkIncompatible(c.SubnetEVMTimestamp, newcfg.SubnetEVMTimestamp, headTimestamp) {
		return newCompatError("SubnetEVM fork block timestamp", c.SubnetEVMTimestamp, newcfg.SubnetEVMTimestamp)
	}
	currentTime := big.NewInt(time.Now().Unix())
	if match, n := matchNil(c.AllowListConfig, newcfg.AllowListConfig); match && !n {
		if isForkIncompatible(c.AllowListConfig.Timestamp(), newcfg.AllowListConfig.Timestamp(), headTimestamp) {
			return newCompatError("AllowList fork block timestamp", c.AllowListConfig.Timestamp(), newcfg.AllowListConfig.Timestamp())
		}
	} else if !match {
		return newCompatError("AllowList", currentTime, currentTime)
	}

	// Check compatibility of misc, non-timed configs ([currentTime] returns on
	// incompatibility to force error propagation)
	if match, _ := matchNil(c.FeeConfig, newcfg.FeeConfig); !match {
		return newCompatError("FeeConfig", currentTime, currentTime)
	}
	if c.AllowFeeRecipients != newcfg.AllowFeeRecipients {
		return newCompatError("AllowFeeRecipients", currentTime, currentTime)
	}

	return nil
}

// isForkIncompatible returns true if a fork scheduled at s1 cannot be rescheduled to
// block s2 because head is already past the fork.
func isForkIncompatible(s1, s2, head *big.Int) bool {
	return (utils.IsForked(s1, head) || utils.IsForked(s2, head)) && !configNumEqual(s1, s2)
}

func configNumEqual(x, y *big.Int) bool {
	if x == nil {
		return y == nil
	}
	if y == nil {
		return x == nil
	}
	return x.Cmp(y) == 0
}

// ConfigCompatError is raised if the locally-stored blockchain is initialised with a
// ChainConfig that would alter the past.
type ConfigCompatError struct {
	What string
	// block numbers of the stored and new configurations
	StoredConfig, NewConfig *big.Int
	// the block number to which the local chain must be rewound to correct the error
	RewindTo uint64
}

func newCompatError(what string, storedblock, newblock *big.Int) *ConfigCompatError {
	var rew *big.Int
	switch {
	case storedblock == nil:
		rew = newblock
	case newblock == nil || storedblock.Cmp(newblock) < 0:
		rew = storedblock
	default:
		rew = newblock
	}
	err := &ConfigCompatError{what, storedblock, newblock, 0}
	if rew != nil && rew.Sign() > 0 {
		err.RewindTo = rew.Uint64() - 1
	}
	return err
}

func (err *ConfigCompatError) Error() string {
	return fmt.Sprintf("mismatching %s in database (have %d, want %d, rewindto %d)", err.What, err.StoredConfig, err.NewConfig, err.RewindTo)
}

// Rules wraps ChainConfig and is merely syntactic sugar or can be used for functions
// that do not have or require information about the block.
//
// Rules is a one time interface meaning that it shouldn't be used in between transition
// phases.
type Rules struct {
	ChainID                                                 *big.Int
	IsHomestead, IsEIP150, IsEIP155, IsEIP158               bool
	IsByzantium, IsConstantinople, IsPetersburg, IsIstanbul bool

	// Rules for Avalanche releases
	IsSubnetEVM bool

	// Optional stateful precompile rules
	IsAllowListEnabled bool
}

// Rules ensures c's ChainID is not nil.
func (c *ChainConfig) rules(num *big.Int) Rules {
	chainID := c.ChainID
	if chainID == nil {
		chainID = new(big.Int)
	}
	return Rules{
		ChainID:          new(big.Int).Set(chainID),
		IsHomestead:      c.IsHomestead(num),
		IsEIP150:         c.IsEIP150(num),
		IsEIP155:         c.IsEIP155(num),
		IsEIP158:         c.IsEIP158(num),
		IsByzantium:      c.IsByzantium(num),
		IsConstantinople: c.IsConstantinople(num),
		IsPetersburg:     c.IsPetersburg(num),
		IsIstanbul:       c.IsIstanbul(num),
	}
}

// AvalancheRules returns the Avalanche modified rules to support Avalanche
// network upgrades
func (c *ChainConfig) AvalancheRules(blockNum, blockTimestamp *big.Int) Rules {
	rules := c.rules(blockNum)

	rules.IsSubnetEVM = c.IsSubnetEVM(blockTimestamp)
	rules.IsAllowListEnabled = c.IsAllowList(blockTimestamp)
	return rules
}

// enabledStatefulPrecompiles returns a list of stateful precompile configs in the order that they are enabled
// by block timestamp.
func (c *ChainConfig) enabledStatefulPrecompiles() []precompile.StatefulPrecompileConfig {
	statefulPrecompileConfigs := make([]precompile.StatefulPrecompileConfig, 0)

	if c.AllowListConfig != nil {
		statefulPrecompileConfigs = append(statefulPrecompileConfigs, c.AllowListConfig)
	}

	return statefulPrecompileConfigs
}

// CheckConfigurePrecompiles iterates over any stateful precompile configs that go into effect at some point and configures them
// if they are activated between [parentTimestamp] and [currentTimestamp].
func (c *ChainConfig) CheckConfigurePrecompiles(parentTimestamp *big.Int, currentTimestamp *big.Int, statedb precompile.StateDB) {
	// Iterate the enabled stateful precompiles and configure them if needed
	for _, config := range c.enabledStatefulPrecompiles() {
		log.Info("CheckConfigurePrecompiles", "parentTimestamp", parentTimestamp, "currentTimestamp", currentTimestamp, "config", config)
		precompile.CheckConfigure(parentTimestamp, currentTimestamp, config, statedb)
	}
}
