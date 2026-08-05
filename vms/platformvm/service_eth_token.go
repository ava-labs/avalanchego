// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ethcommon "github.com/ava-labs/libevm/common"
	ethtypes "github.com/ava-labs/libevm/core/types"
)

// The staked-position virtual ERC-20. It exists only in this RPC layer: balanceOf
// reports the caller's active eth-authorized stake, in the same 18-decimal
// scale as eth_getBalance (the facade scales nAVAX by 1e9 everywhere, so 1
// staked-token unit displays as 1 AVAX staked). Transfers are impossible: consensus
// rejects any tx targeting the token address.
const (
	stakedTokenName     = "Staked AVAX (P-Chain)"
	stakedTokenSymbol   = "STAKED"
	stakedTokenDecimals = 18
)

// Standard ERC-20 selectors, pinned by TestERC20Selectors.
var (
	selectorName        = [4]byte{0x06, 0xfd, 0xde, 0x03} // name()
	selectorSymbol      = [4]byte{0x95, 0xd8, 0x9b, 0x41} // symbol()
	selectorDecimals    = [4]byte{0x31, 0x3c, 0xe5, 0x67} // decimals()
	selectorTotalSupply = [4]byte{0x18, 0x16, 0x0d, 0xdd} // totalSupply()
	selectorBalanceOf   = [4]byte{0x70, 0xa0, 0x82, 0x31} // balanceOf(address)

	// transferTopic is keccak256("Transfer(address,address,uint256)"), pinned
	// by TestERC20Selectors.
	transferTopic = ethcommon.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
)

func (a *ethAPI) ethCall(to *ethcommon.Address, data []byte) (any, error) {
	// Anything but the token behaves like a call to an empty account.
	if to == nil || *to != txs.EthStakedAVAXAddress {
		return "0x", nil
	}
	if len(data) < 4 {
		return nil, fmt.Errorf("calldata too short for a token call")
	}
	var selector [4]byte
	copy(selector[:], data)
	args := data[4:]

	switch selector {
	case selectorName:
		return abiEncodeString(stakedTokenName), nil
	case selectorSymbol:
		return abiEncodeString(stakedTokenSymbol), nil
	case selectorDecimals:
		return abiEncodeUint(big.NewInt(stakedTokenDecimals)), nil
	case selectorTotalSupply:
		total, err := a.stakedNAVAX(nil)
		if err != nil {
			return nil, err
		}
		return abiEncodeUint(navaxToWei(total)), nil
	case selectorBalanceOf:
		if len(args) != 32 {
			return nil, fmt.Errorf("balanceOf takes exactly one address argument")
		}
		if !allZeroBytes(args[:12]) {
			return nil, fmt.Errorf("non-zero padding in the address argument")
		}
		addr := ids.ShortID(args[12:32])
		staked, err := a.stakedNAVAX(&addr)
		if err != nil {
			return nil, err
		}
		return abiEncodeUint(navaxToWei(staked)), nil
	default:
		return nil, fmt.Errorf("unknown token selector %x", selector)
	}
}

// stakedBalances is one block's worth of eth-authorized stake, computed in a
// single walk of the staker set and reused by every token read at that height.
type stakedBalances struct {
	blkID   ids.ID
	byOwner map[ids.ShortID]uint64
	total   uint64
}

// stakedNAVAX returns the eth-authorized stake of [owner], or the total when
// owner is nil. The staker-set walk happens at most once per accepted block,
// so a flood of token reads costs one walk rather than one per request.
func (a *ethAPI) stakedNAVAX(owner *ids.ShortID) (uint64, error) {
	if lastAccepted := a.vm.state.GetLastAccepted(); a.stakedCache.byOwner == nil ||
		a.stakedCache.blkID != lastAccepted {
		balances, err := a.walkStakedBalances()
		if err != nil {
			return 0, err
		}
		balances.blkID = lastAccepted
		a.stakedCache = balances
	}
	if owner == nil {
		return a.stakedCache.total, nil
	}
	return a.stakedCache.byOwner[*owner], nil
}

// walkStakedBalances sums the weight of active eth-authorized stakers per
// reward owner. Eth-authorized staker txs are recognizable by declaring inputs
// while carrying no credentials: native staker txs always carry credentials,
// and genesis staker txs carry neither credentials nor inputs.
func (a *ethAPI) walkStakedBalances() (stakedBalances, error) {
	balances := stakedBalances{byOwner: make(map[ids.ShortID]uint64)}
	it, err := a.vm.state.GetCurrentStakerIterator()
	if err != nil {
		return balances, err
	}
	defer it.Release()

	for it.Next() {
		staker := it.Value()
		tx, _, err := a.vm.state.GetTx(staker.TxID)
		if err != nil {
			continue // system stakers (genesis validators) have no stored tx
		}
		if len(tx.Creds) != 0 {
			continue
		}
		var rewardsOwner *secp256k1fx.OutputOwners
		switch unsigned := tx.Unsigned.(type) {
		case *txs.AddPermissionlessDelegatorTx:
			if len(unsigned.Ins) == 0 {
				continue // genesis staker
			}
			rewardsOwner, _ = unsigned.DelegationRewardsOwner.(*secp256k1fx.OutputOwners)
		case *txs.AddPermissionlessValidatorTx:
			if len(unsigned.Ins) == 0 {
				continue // genesis staker
			}
			rewardsOwner, _ = unsigned.ValidatorRewardsOwner.(*secp256k1fx.OutputOwners)
		default:
			continue
		}
		if rewardsOwner == nil || len(rewardsOwner.Addrs) != 1 {
			continue
		}
		balances.byOwner[rewardsOwner.Addrs[0]] += staker.Weight
		balances.total += staker.Weight
	}
	return balances, nil
}

// stakeLogs synthesizes the logs of an accepted eth tx: staking calls emit one
// staked-token Transfer mint (from the zero address to the staker, value = staked
// amount). Plain transfers emit nothing.
func stakeLogs(unsigned *txs.EthRLPTx, record *ethReceiptRecord, ethHash ethcommon.Hash) []*ethtypes.Log {
	if !unsigned.IsStakingCall() {
		// Non-nil so JSON callers get [] rather than null; geth's receipt
		// decoder requires the field.
		return []*ethtypes.Log{}
	}
	value := navaxToWei(unsigned.AmountNAVAX)
	return []*ethtypes.Log{{
		Address: txs.EthStakedAVAXAddress,
		Topics: []ethcommon.Hash{
			transferTopic,
			ethcommon.Hash{}, // from: the zero address (mint)
			addressTopic(ethcommon.Address(unsigned.Sender)),
		},
		Data:        ethcommon.LeftPadBytes(value.Bytes(), 32),
		BlockNumber: record.height,
		BlockHash:   ethcommon.Hash(record.blkID),
		TxHash:      ethHash,
		TxIndex:     uint(record.txIndex),
		Index:       uint(record.txIndex), // one log per tx keeps this unique
	}}
}

// logFilter is the eth_getLogs filter object.
type logFilter struct {
	FromBlock string            `json:"fromBlock"`
	ToBlock   string            `json:"toBlock"`
	BlockHash *ethcommon.Hash   `json:"blockHash"`
	Address   json.RawMessage   `json:"address"`
	Topics    []json.RawMessage `json:"topics"`
}

// maxGetLogsBlocks bounds an eth_getLogs scan.
const maxGetLogsBlocks = 2048

func (a *ethAPI) getLogs(filter *logFilter) (any, error) {
	var fromHeight, toHeight uint64
	if filter.BlockHash != nil {
		blk, err := a.vm.state.GetStatelessBlock(ids.ID(*filter.BlockHash))
		if err != nil {
			return nil, err
		}
		fromHeight = blk.Height()
		toHeight = fromHeight
	} else {
		var ok bool
		var err error
		fromHeight, ok, err = a.resolveBlockTag(orLatest(filter.FromBlock))
		if err != nil || !ok {
			return nil, err
		}
		toHeight, ok, err = a.resolveBlockTag(orLatest(filter.ToBlock))
		if err != nil || !ok {
			return nil, err
		}
	}
	if toHeight < fromHeight {
		return []any{}, nil
	}
	if toHeight-fromHeight >= maxGetLogsBlocks {
		return nil, fmt.Errorf("eth_getLogs range is capped at %d blocks", maxGetLogsBlocks)
	}

	addresses, err := filterAddresses(filter.Address)
	if err != nil {
		return nil, err
	}

	logs := []*ethtypes.Log{}
	for h := fromHeight; h <= toHeight; h++ {
		blkID, err := a.vm.state.GetBlockIDAtHeight(h)
		if err != nil {
			return nil, err
		}
		blk, err := a.vm.state.GetStatelessBlock(blkID)
		if err != nil {
			return nil, err
		}
		for _, tx := range blk.Txs() {
			unsigned, ok := tx.Unsigned.(*txs.EthRLPTx)
			if !ok {
				continue
			}
			if err := unsigned.SyntacticVerify(a.vm.ctx); err != nil {
				return nil, err
			}
			record, found, err := a.receiptRecord(tx.ID())
			if err != nil {
				return nil, err
			}
			if !found {
				if err := a.scanAcceptedBlocks(); err != nil {
					return nil, err
				}
				if record, found, err = a.receiptRecord(tx.ID()); err != nil || !found {
					return nil, fmt.Errorf("accepted eth tx is not indexed: %w", err)
				}
			}
			for _, log := range stakeLogs(unsigned, &record, unsigned.Parsed.Hash()) {
				if matchesFilter(log, addresses, filter.Topics) {
					logs = append(logs, log)
				}
			}
		}
	}
	return logs, nil
}

func orLatest(tag string) string {
	if tag == "" {
		return "latest"
	}
	return tag
}

// filterAddresses parses the address field, which may be a single address or
// an array. Empty means no address filter.
func filterAddresses(raw json.RawMessage) ([]ethcommon.Address, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var one ethcommon.Address
	if err := json.Unmarshal(raw, &one); err == nil {
		return []ethcommon.Address{one}, nil
	}
	var many []ethcommon.Address
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, fmt.Errorf("bad address filter: %w", err)
	}
	return many, nil
}

// matchesFilter applies standard eth_getLogs semantics: address must be in the
// set (if any), and topics[i] must equal one of the filter's options at
// position i (null matches anything).
func matchesFilter(log *ethtypes.Log, addresses []ethcommon.Address, topics []json.RawMessage) bool {
	if len(addresses) > 0 {
		found := false
		for _, addr := range addresses {
			if addr == log.Address {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(topics) > len(log.Topics) {
		return false
	}
	for i, raw := range topics {
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var one ethcommon.Hash
		if err := json.Unmarshal(raw, &one); err == nil {
			if one != log.Topics[i] {
				return false
			}
			continue
		}
		var options []ethcommon.Hash
		if err := json.Unmarshal(raw, &options); err != nil {
			return false
		}
		matched := false
		for _, option := range options {
			if option == log.Topics[i] {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func logsBloomHex(logs []*ethtypes.Log) string {
	var bloom ethtypes.Bloom
	for _, log := range logs {
		bloom.Add(log.Address.Bytes())
		for _, topic := range log.Topics {
			bloom.Add(topic.Bytes())
		}
	}
	return "0x" + hex.EncodeToString(bloom.Bytes())
}

func addressTopic(addr ethcommon.Address) ethcommon.Hash {
	var topic ethcommon.Hash
	copy(topic[12:], addr[:])
	return topic
}

func navaxToWei(navax uint64) *big.Int {
	return new(big.Int).Mul(new(big.Int).SetUint64(navax), txs.WeiPerNAVAX)
}

func abiEncodeUint(v *big.Int) string {
	return "0x" + hex.EncodeToString(ethcommon.LeftPadBytes(v.Bytes(), 32))
}

// abiEncodeString ABI-encodes a single string return value.
func abiEncodeString(s string) string {
	length := len(s)
	padded := (length + 31) / 32 * 32
	out := make([]byte, 64+padded)
	out[31] = 32 // offset of the dynamic value
	copy(out[32:], ethcommon.LeftPadBytes(big.NewInt(int64(length)).Bytes(), 32))
	copy(out[64:], s)
	return "0x" + hex.EncodeToString(out)
}

func allZeroBytes(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
