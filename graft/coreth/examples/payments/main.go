package main

import (
    //"time"
    "os"
    "os/signal"
    "syscall"
    "crypto/rand"
    "math/big"
    //"encoding/hex"
    "github.com/ethereum/go-ethereum/core/types"
    //"github.com/ethereum/go-ethereum/common"
    "github.com/ethereum/go-ethereum/common/hexutil"
    "github.com/ethereum/go-ethereum/core"
    "github.com/Determinant/coreth/eth"
    "github.com/Determinant/coreth"
    "github.com/ethereum/go-ethereum/log"
    "github.com/ethereum/go-ethereum/params"
)

func checkError(err error) {
    if err != nil { panic(err) }
}

func main() {
    log.Root().SetHandler(log.StderrHandler)
    config := eth.DefaultConfig
    chainConfig := params.MainnetChainConfig

    genBalance := big.NewInt(1000000000000000000)
    genKey, _ := coreth.NewKey(rand.Reader)

    config.Genesis = &core.Genesis{
        Config:     params.MainnetChainConfig,
        Nonce:      0,
        ExtraData:  hexutil.MustDecode("0x00"),
        GasLimit:   100000000,
        Difficulty: big.NewInt(0),
        Alloc: core.GenesisAlloc{ genKey.Address: { Balance: genBalance }},
    }

    chainID := chainConfig.ChainID
    nonce := uint64(0)
    value := big.NewInt(1000000000000)
    gasLimit := 21000
    gasPrice := big.NewInt(1000)
    bob, err := coreth.NewKey(rand.Reader); checkError(err)
    tx := types.NewTransaction(nonce, bob.Address, value, uint64(gasLimit), gasPrice, nil)
    signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey); checkError(err)

    chain := coreth.NewETHChain(&config, chainConfig, nil)
    chain.Start()
    chain.AddLocalTxs([]*types.Transaction{signedTx})

    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    signal.Notify(c, os.Interrupt, syscall.SIGINT)
    <-c
    chain.Stop()
}
