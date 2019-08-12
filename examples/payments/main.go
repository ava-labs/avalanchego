package main

import (
    "math/big"
    "encoding/hex"
    "github.com/ethereum/go-ethereum/core/types"
    "github.com/ethereum/go-ethereum/common"
    "github.com/ethereum/go-ethereum/common/hexutil"
    "github.com/ethereum/go-ethereum/core"
    "github.com/Determinant/coreth/eth"
    "github.com/Determinant/coreth"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
    "time"
)

func main() {
    log.Root().SetHandler(log.StdoutHandler)
    config := eth.DefaultConfig
    genAddr := common.Address{}
    genBalance := big.NewInt(1000000000000000000)
    config.Genesis = &core.Genesis{
        Config:     params.MainnetChainConfig,
        Nonce:      66,
        ExtraData:  hexutil.MustDecode("0x11bbe8db4e347b4e8c937c1c8370e4b5ed33adb3db69cbdb7a38e1e50b1b82fa"),
        GasLimit:   5000,
        Difficulty: big.NewInt(0),
        Alloc: core.GenesisAlloc{genAddr: {Balance: genBalance }},
    }
    chain := coreth.NewETHChain(&config, nil, nil)
    to := common.Address{}
    nouce := 0
    amount := big.NewInt(0)
    gasLimit := 1000000000
    gasPrice := big.NewInt(0)
	deployCode, _ := hex.DecodeString("608060405234801561001057600080fd5b50600760008190555060cc806100276000396000f3fe6080604052600436106039576000357c0100000000000000000000000000000000000000000000000000000000900480631003e2d214603e575b600080fd5b348015604957600080fd5b50607360048036036020811015605e57600080fd5b81019080803590602001909291905050506089565b6040518082815260200191505060405180910390f35b60008160005401600081905550600054905091905056fea165627a7a7230582075069a1c11ef20dd272178c92ff7d593d7ef9c39b1a63e85588f9e45be9fb6420029")
    tx := types.NewTransaction(uint64(nouce), to, amount, uint64(gasLimit), gasPrice, deployCode)
    chain.Start()
    //_ = tx
    chain.AddLocalTxs([]*types.Transaction{tx})
    time.Sleep(10000 * time.Millisecond)
    chain.Stop()
}
