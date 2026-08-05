// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	ethtypes "github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ethcommon "github.com/ava-labs/libevm/common"
	ethcrypto "github.com/ava-labs/libevm/crypto"
)

func ethCallAPI(t *testing.T, api *ethAPI, method string, params ...any) any {
	t.Helper()
	rawParams := make([]json.RawMessage, len(params))
	for i, p := range params {
		raw, err := json.Marshal(p)
		require.NoError(t, err)
		rawParams[i] = raw
	}
	result, err := api.call(&ethRequest{Method: method, Params: rawParams})
	require.NoError(t, err)
	return result
}

// fundAndSign funds a fresh eth key in vm state and returns it.
func fundEthKey(t *testing.T, vm *VM, amount uint64) *secp256k1.PrivateKey {
	t.Helper()
	key, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)
	vm.state.AddUTXO(&avax.UTXO{
		UTXOID: avax.UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 0},
		Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.ShortID(key.PublicKey().EthAddress())},
			},
		},
	})
	require.NoError(t, vm.state.Commit())
	return key
}

func signedTransferRLP(t *testing.T, vm *VM, key *secp256k1.PrivateKey, nonce uint64, to ethcommon.Address, amountNAVAX uint64, calldata []byte) string {
	t.Helper()
	return signedTransferRLPWithGas(t, vm, key, nonce, to, amountNAVAX, calldata, 10_000_000)
}

func signedTransferRLPWithGas(t *testing.T, vm *VM, key *secp256k1.PrivateKey, nonce uint64, to ethcommon.Address, amountNAVAX uint64, calldata []byte, gasLimit uint64) string {
	t.Helper()
	chainID := txs.EthRLPChainID(vm.ctx.NetworkID)
	signed := ethtypes.MustSignNewTx(
		key.ToECDSA(),
		ethtypes.LatestSignerForChainID(chainID),
		&ethtypes.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     nonce,
			GasTipCap: big.NewInt(0),
			GasFeeCap: big.NewInt(2_000_000_000),
			Gas:       gasLimit,
			To:        &to,
			Value:     new(big.Int).Mul(new(big.Int).SetUint64(amountNAVAX), txs.WeiPerNAVAX),
			Data:      calldata,
		},
	)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)
	return fmt.Sprintf("0x%x", raw)
}

// buildAndAccept builds a block from the mempool and accepts it.
func buildAndAccept(t *testing.T, vm *VM) {
	t.Helper()
	blk, err := vm.Builder.BuildBlock(t.Context())
	require.NoError(t, err)
	require.NoError(t, blk.Verify(t.Context()))
	require.NoError(t, blk.Accept(t.Context()))
	require.NoError(t, vm.SetPreference(t.Context(), vm.manager.LastAccepted()))
}

// TestEthAPIWalletFlow drives the RPC sequence a wallet performs: probe chain
// and fees, send, poll tx/receipt/block, read the stAVAX token and its logs,
// checking that no two views contradict each other.
func TestEthAPIWalletFlow(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Latest)

	api := newEthAPI(vm)
	var key *secp256k1.PrivateKey
	locked(vm, func() { key = fundEthKey(t, vm, 1000*units.Avax) })
	sender := ethcommon.Address(key.PublicKey().EthAddress())
	recipient := ethcommon.Address(ids.GenerateTestShortID())

	// Chain probes.
	require.Equal(
		"0x"+txs.EthRLPChainID(vm.ctx.NetworkID).Text(16),
		ethCallAPI(t, api, "eth_chainId"),
	)
	require.Equal("0x0", ethCallAPI(t, api, "eth_maxPriorityFeePerGas"))
	latest := ethCallAPI(t, api, "eth_getBlockByNumber", "latest", false).(map[string]any)
	require.Contains(latest, "baseFeePerGas") // MetaMask's EIP-1559 probe
	feeHistory := ethCallAPI(t, api, "eth_feeHistory", "0x5", "latest", []float64{25, 75}).(map[string]any)
	require.Len(feeHistory["baseFeePerGas"], 6)
	require.Len(feeHistory["gasUsedRatio"], 5)

	// Send a plain transfer and accept a block with it.
	raw := signedTransferRLP(t, vm, key, 0, recipient, 5*units.Avax, nil)
	txHash := ethCallAPI(t, api, "eth_sendRawTransaction", raw).(string)

	// Pending: tx resolves with null block fields.
	pending := ethCallAPI(t, api, "eth_getTransactionByHash", txHash).(map[string]any)
	require.Nil(pending["blockNumber"])

	locked(vm, func() { buildAndAccept(t, vm) })

	// Receipt, tx and block must agree.
	receipt := ethCallAPI(t, api, "eth_getTransactionReceipt", txHash).(map[string]any)
	require.Equal("0x1", receipt["status"])
	blockNumber := receipt["blockNumber"].(string)
	blockHash := receipt["blockHash"].(string)

	txObj := ethCallAPI(t, api, "eth_getTransactionByHash", txHash).(map[string]any)
	require.Equal(blockNumber, txObj["blockNumber"])
	require.Equal(blockHash, txObj["blockHash"])

	byNumber := ethCallAPI(t, api, "eth_getBlockByNumber", blockNumber, false).(map[string]any)
	require.Equal(blockHash, byNumber["hash"])
	require.Contains(byNumber["transactions"], txHash)

	byHash := ethCallAPI(t, api, "eth_getBlockByHash", blockHash, true).(map[string]any)
	require.Equal(blockNumber, byHash["number"])
	fullTxs := byHash["transactions"].([]any)
	require.Len(fullTxs, 1)
	require.Equal(txHash, fullTxs[0].(map[string]any)["hash"])
	require.Equal(sender.Hex(), fullTxs[0].(map[string]any)["from"])

	// A transfer emits no logs.
	require.Empty(receipt["logs"])

	// Delegate, then check token views and logs.
	nodeID := genesistest.DefaultNodeIDs[0]
	calldata := txs.SelectorDelegate[:]
	nodeWord := make([]byte, 32)
	copy(nodeWord, nodeID[:])
	calldata = append(calldata, nodeWord...)
	endWord := make([]byte, 32)
	binary.BigEndian.PutUint64(endWord[24:], genesistest.DefaultValidatorEndTimeUnix)
	calldata = append(calldata, endWord...)

	const stake = 4 * units.MilliAvax
	raw = signedTransferRLP(t, vm, key, 1, txs.EthStakingAddress, stake, calldata)
	stakeHash := ethCallAPI(t, api, "eth_sendRawTransaction", raw).(string)
	locked(vm, func() { buildAndAccept(t, vm) })

	stakeReceipt := ethCallAPI(t, api, "eth_getTransactionReceipt", stakeHash).(map[string]any)
	require.Equal("0x1", stakeReceipt["status"])
	logs := stakeReceipt["logs"].([]*ethtypes.Log)
	require.Len(logs, 1)
	require.Equal(txs.EthStakedAVAXAddress, logs[0].Address)
	require.Equal(transferTopic, logs[0].Topics[0])
	require.Equal(addressTopic(sender), logs[0].Topics[2])
	require.Equal(navaxToWei(stake).Bytes(), new(big.Int).SetBytes(logs[0].Data).Bytes())
	require.NotEqual(logsBloomHex(nil), stakeReceipt["logsBloom"])

	// balanceOf and totalSupply report the staked amount in wei scale.
	balanceOfCalldata := "0x70a08231" + "000000000000000000000000" + fmt.Sprintf("%x", sender)
	balance := ethCallAPI(t, api, "eth_call", map[string]string{
		"to":   txs.EthStakedAVAXAddress.Hex(),
		"data": balanceOfCalldata,
	}, "latest").(string)
	require.Equal(navaxToWei(stake), hexToBig(t, balance))
	supply := ethCallAPI(t, api, "eth_call", map[string]string{
		"to":   txs.EthStakedAVAXAddress.Hex(),
		"data": "0x18160ddd",
	}, "latest").(string)
	require.Equal(navaxToWei(stake), hexToBig(t, supply))

	// name, symbol and decimals decode.
	require.Equal(big.NewInt(stAVAXDecimals), hexToBig(t, ethCallAPI(t, api, "eth_call", map[string]string{
		"to": txs.EthStakedAVAXAddress.Hex(), "data": "0x313ce567",
	}, "latest").(string)))
	nameResult := ethCallAPI(t, api, "eth_call", map[string]string{
		"to": txs.EthStakedAVAXAddress.Hex(), "data": "0x06fdde03",
	}, "latest").(string)
	require.Contains(decodeABIString(t, nameResult), "Staked AVAX")

	// eth_getLogs finds the mint by address and by topic.
	foundLogs := ethCallAPI(t, api, "eth_getLogs", map[string]any{
		"fromBlock": "0x0",
		"toBlock":   "latest",
		"address":   txs.EthStakedAVAXAddress.Hex(),
		"topics":    []any{transferTopic.Hex(), nil, addressTopic(sender).Hex()},
	}).([]*ethtypes.Log)
	require.Len(foundLogs, 1)
	require.Equal(logs[0].TxHash, foundLogs[0].TxHash)

	// A mismatched topic filter finds nothing.
	noLogs := ethCallAPI(t, api, "eth_getLogs", map[string]any{
		"fromBlock": "0x0",
		"toBlock":   "latest",
		"topics":    []any{transferTopic.Hex(), nil, addressTopic(recipient).Hex()},
	}).([]*ethtypes.Log)
	require.Empty(noLogs)

	// eth_call to a non-token address is an empty result, not an error.
	require.Equal("0x", ethCallAPI(t, api, "eth_call", map[string]string{
		"to": recipient.Hex(), "data": "0x70a08231",
	}, "latest"))

	// A transfer to the token address is rejected at admission.
	raw = signedTransferRLP(t, vm, key, 2, txs.EthStakedAVAXAddress, units.Avax, nil)
	_, err := api.call(&ethRequest{Method: "eth_sendRawTransaction", Params: []json.RawMessage{mustJSON(t, raw)}})
	require.ErrorIs(err, txs.ErrTransferToToken)
}

func locked(vm *VM, f func()) {
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()
	f()
}

// TestERC20Selectors pins the hardcoded selectors and event topic to their
// keccak definitions.
func TestERC20Selectors(t *testing.T) {
	for signature, selector := range map[string][4]byte{
		"name()":             selectorName,
		"symbol()":           selectorSymbol,
		"decimals()":         selectorDecimals,
		"totalSupply()":      selectorTotalSupply,
		"balanceOf(address)": selectorBalanceOf,
	} {
		require.Equal(t, ethcrypto.Keccak256([]byte(signature))[:4], selector[:], signature)
	}
	require.Equal(t,
		ethcrypto.Keccak256([]byte("Transfer(address,address,uint256)")),
		transferTopic.Bytes(),
	)
}

func TestEthCallTokenRejections(t *testing.T) {
	vm, _, _ := defaultVM(t, upgradetest.Latest)
	api := newEthAPI(vm)

	tests := []struct {
		name string
		data string
	}{
		{name: "empty calldata", data: "0x"},
		{name: "unknown selector", data: "0xdeadbeef"},
		{name: "balanceOf without argument", data: "0x70a08231"},
		{name: "balanceOf with short argument", data: "0x70a08231" + "ff"},
		{
			name: "balanceOf with dirty padding",
			data: "0x70a08231" + "01" + fmt.Sprintf("%062x", 0),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := api.call(&ethRequest{
				Method: "eth_call",
				Params: []json.RawMessage{
					mustJSON(t, map[string]string{
						"to":   txs.EthStakedAVAXAddress.Hex(),
						"data": tt.data,
					}),
					mustJSON(t, "latest"),
				},
			})
			require.Error(t, err)
		})
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}

func hexToBig(t *testing.T, s string) *big.Int {
	t.Helper()
	v, ok := new(big.Int).SetString(s[2:], 16)
	require.True(t, ok)
	return v
}

func decodeABIString(t *testing.T, s string) string {
	t.Helper()
	raw := ethcommon.FromHex(s)
	require.GreaterOrEqual(t, len(raw), 64)
	length := new(big.Int).SetBytes(raw[32:64]).Int64()
	require.LessOrEqual(t, 64+length, int64(len(raw)))
	return string(raw[64 : 64+length])
}

// An unauthenticated receipt lookup must not scan the whole chain. The
// watermark starts at the tip on first use, so a public node cannot be made to
// walk its entire block history under the chain lock.
func TestEthAPIReceiptScanStartsAtTip(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Latest)

	// Produce history before the API exists, as a node syncing then serving
	// requests would.
	bootstrap := newEthAPI(vm)
	var key *secp256k1.PrivateKey
	locked(vm, func() { key = fundEthKey(t, vm, 1000*units.Avax) })
	raw := signedTransferRLP(t, vm, key, 0, ethcommon.Address(ids.GenerateTestShortID()), units.Avax, nil)
	ethCallAPI(t, bootstrap, "eth_sendRawTransaction", raw)
	locked(vm, func() { buildAndAccept(t, vm) })

	// Wipe the index to model a node whose API has never run, then construct
	// the API: it must seed at the tip rather than walk the chain.
	locked(vm, func() {
		require.NoError(bootstrap.indexDB.Delete(ethWatermarkKey))
	})
	var (
		api    *ethAPI
		height uint64
	)
	locked(vm, func() {
		api = newEthAPI(vm)
		var err error
		height, err = api.lastAcceptedHeight()
		require.NoError(err)
	})
	watermark, err := database.GetUInt64(api.indexDB, ethWatermarkKey)
	require.NoError(err)
	require.Equal(height, watermark, "construction must not walk history")

	// Txs submitted from now on are still indexed and served.
	raw = signedTransferRLP(t, vm, key, 1, ethcommon.Address(ids.GenerateTestShortID()), units.Avax, nil)
	hash := ethCallAPI(t, api, "eth_sendRawTransaction", raw).(string)
	locked(vm, func() { buildAndAccept(t, vm) })
	require.NotNil(ethCallAPI(t, api, "eth_getTransactionReceipt", hash))
}

// A failed or spammed submission must not write to disk, or an unauthenticated
// caller could grow the node's database without limit.
func TestEthAPIRejectedSubmissionWritesNothing(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Latest)

	api := newEthAPI(vm)
	var key *secp256k1.PrivateKey
	locked(vm, func() { key = fundEthKey(t, vm, 1000*units.Avax) })

	countRows := func() int {
		it := api.indexDB.NewIterator()
		defer it.Release()
		rows := 0
		for it.Next() {
			rows++
		}
		return rows
	}
	before := countRows()

	// A tx to the read-only token address is rejected at admission.
	raw := signedTransferRLP(t, vm, key, 0, ethcommon.Address(txs.EthStakedAVAXAddress), units.Avax, nil)
	_, err := api.call(&ethRequest{
		Method: "eth_sendRawTransaction",
		Params: []json.RawMessage{mustJSON(t, raw)},
	})
	require.ErrorIs(err, txs.ErrTransferToToken)
	require.Equal(before, countRows(), "a rejected submission persisted index rows")
}

// eth_getBalance must equal what one tx can actually spend under the selection
// rule, so a wallet's Max button proposes a tx that executes.
func TestEthAPIBalanceIsSpendable(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Latest)

	api := newEthAPI(vm)
	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	addr := ids.ShortID(key.PublicKey().EthAddress())

	// MaxEthRLPTxInputs+4 equal UTXOs: only the first MaxEthRLPTxInputs of them
	// can be spent by one tx, so that is the balance.
	const each = units.Avax
	locked(vm, func() {
		for i := 0; i < txs.MaxEthRLPTxInputs+4; i++ {
			vm.state.AddUTXO(&avax.UTXO{
				UTXOID: avax.UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 0},
				Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: each,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{addr},
					},
				},
			})
		}
		require.NoError(vm.state.Commit())
	})

	balance := ethCallAPI(t, api, "eth_getBalance", ethcommon.Address(addr).Hex(), "latest").(string)
	require.Equal(navaxToWei(txs.MaxEthRLPTxInputs*each), hexToBig(t, balance))
}

// Token reads walk the staker set at most once per accepted block, so a flood
// of balanceOf calls cannot be turned into a flood of walks.
func TestEthAPIStakedBalanceCachedPerBlock(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Latest)

	api := newEthAPI(vm)
	addr := ids.GenerateTestShortID()

	locked(vm, func() {
		_, err := api.stakedNAVAX(&addr)
		require.NoError(err)
	})
	firstBlkID := api.stakedCache.blkID
	require.NotEqual(ids.Empty, firstBlkID)

	// A second read at the same height reuses the cached walk.
	locked(vm, func() {
		api.stakedCache.total = 12345 // poison it: a re-walk would clear this
		_, err := api.stakedNAVAX(nil)
		require.NoError(err)
	})
	require.Equal(uint64(12345), api.stakedCache.total)

	// A new block invalidates it.
	var key *secp256k1.PrivateKey
	locked(vm, func() { key = fundEthKey(t, vm, 100*units.Avax) })
	raw := signedTransferRLP(t, vm, key, 0, ethcommon.Address(ids.GenerateTestShortID()), units.Avax, nil)
	ethCallAPI(t, api, "eth_sendRawTransaction", raw)
	locked(vm, func() {
		buildAndAccept(t, vm)
		_, err := api.stakedNAVAX(nil)
		require.NoError(err)
	})
	require.NotEqual(firstBlkID, api.stakedCache.blkID)
	require.NotEqual(uint64(12345), api.stakedCache.total)
}

// eth_estimateGas runs the real selection walk, so a wallet that signs the
// estimate pays exactly it, for both a one-input and a multi-input send.
func TestEthAPIEstimateGasMatchesChargedFee(t *testing.T) {
	for _, tt := range []struct {
		name       string
		utxos      []uint64
		value      uint64
		wantInputs int
	}{
		{
			name:       "one input",
			utxos:      []uint64{100 * units.Avax},
			value:      10 * units.Avax,
			wantInputs: 1,
		},
		{
			name:       "three inputs",
			utxos:      []uint64{10 * units.Avax, 10 * units.Avax, 10 * units.Avax},
			value:      25 * units.Avax,
			wantInputs: 3,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			vm, _, _ := defaultVM(t, upgradetest.Latest)
			api := newEthAPI(vm)

			key, err := secp256k1.NewPrivateKey()
			require.NoError(err)
			sender := ethcommon.Address(key.PublicKey().EthAddress())
			locked(vm, func() {
				for _, amt := range tt.utxos {
					vm.state.AddUTXO(&avax.UTXO{
						UTXOID: avax.UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 0},
						Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt: amt,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{ids.ShortID(sender)},
							},
						},
					})
				}
				require.NoError(vm.state.Commit())
			})

			recipient := ethcommon.Address(ids.GenerateTestShortID())
			estimate := hexToBig(t, ethCallAPI(t, api, "eth_estimateGas", map[string]any{
				"from":  sender.Hex(),
				"to":    recipient.Hex(),
				"value": "0x" + navaxToWei(tt.value).Text(16),
			}).(string))

			// Sign exactly the estimate and land it.
			raw := signedTransferRLPWithGas(t, vm, key, 0, recipient, tt.value, nil, estimate.Uint64())
			hash := ethCallAPI(t, api, "eth_sendRawTransaction", raw).(string)
			locked(vm, func() { buildAndAccept(t, vm) })

			receipt := ethCallAPI(t, api, "eth_getTransactionReceipt", hash).(map[string]any)
			require.Equal("0x1", receipt["status"])
			gasUsed := hexToBig(t, receipt["gasUsed"].(string))

			// The estimate covers the charge, and the only difference is the
			// unused envelope slack priced as bandwidth, since the exact
			// serialized length is unknowable before signing.
			require.LessOrEqual(gasUsed.Uint64(), estimate.Uint64())
			rlpLen := uint64(len(ethcommon.FromHex(raw)))
			slack := (uint64(txs.MaxEthRLPEnvelopeBytes) - rlpLen) *
				uint64(vm.DynamicFeeConfig.Weights[gas.Bandwidth])
			require.Equal(slack, estimate.Uint64()-gasUsed.Uint64())

			// And it is the gas for the expected number of inputs.
			wantGas, err := fee.EthRLPTxComplexity(
				txs.MaxEthRLPEnvelopeBytes, tt.wantInputs,
			).ToGas(vm.DynamicFeeConfig.Weights)
			require.NoError(err)
			require.Equal(uint64(wantGas), estimate.Uint64())
		})
	}
}

// Wallets probe eth_getCode to decide whether an address is a contract. Plain
// accounts must report no code, and the system addresses must not look like
// ordinary accounts.
func TestEthAPIGetCode(t *testing.T) {
	vm, _, _ := defaultVM(t, upgradetest.Latest)
	api := newEthAPI(vm)

	require.Equal(t, "0x", ethCallAPI(t, api, "eth_getCode",
		ethcommon.Address(ids.GenerateTestShortID()).Hex(), "latest"))
	require.Equal(t, "0xfe", ethCallAPI(t, api, "eth_getCode",
		txs.EthStakingAddress.Hex(), "latest"))
	require.Equal(t, "0xfe", ethCallAPI(t, api, "eth_getCode",
		txs.EthStakedAVAXAddress.Hex(), "latest"))
}
