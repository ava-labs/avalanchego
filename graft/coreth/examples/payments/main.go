package main

import (
    "time"
    "os"
    "os/signal"
    "syscall"
    "crypto/rand"
    "math/big"
    //"encoding/hex"
    "github.com/ethereum/go-ethereum/core/types"
    "github.com/ethereum/go-ethereum/common/hexutil"
    "github.com/ethereum/go-ethereum/core"
    "github.com/Determinant/coreth/eth"
    "github.com/Determinant/coreth"
    "github.com/ethereum/go-ethereum/params"
    "github.com/ethereum/go-ethereum/common"
)

func checkError(err error) {
    if err != nil { panic(err) }
}

func main() {
    config := eth.DefaultConfig
    chainConfig := &params.ChainConfig {
        ChainID:             big.NewInt(1),
        HomesteadBlock:      big.NewInt(0),
        DAOForkBlock:        big.NewInt(0),
        DAOForkSupport:      true,
        EIP150Block:         big.NewInt(0),
        EIP150Hash:          common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
        EIP155Block:         big.NewInt(0),
        EIP158Block:         big.NewInt(0),
        ByzantiumBlock:      big.NewInt(0),
        ConstantinopleBlock: big.NewInt(0),
        PetersburgBlock:     big.NewInt(0),
        IstanbulBlock:       nil,
        Ethash:              nil,
    }

    genBalance := big.NewInt(100000000000000000)
    genKey, _ := coreth.NewKey(rand.Reader)

    config.Genesis = &core.Genesis{
        Config:     chainConfig,
        Nonce:      0,
        Number:     0,
        ExtraData:  hexutil.MustDecode("0x00"),
        GasLimit:   100000000,
        Difficulty: big.NewInt(0),
        Alloc: core.GenesisAlloc{ genKey.Address: { Balance: genBalance }},
    }

    config.Miner.ManualMining = true
    config.Miner.ManualUncle = true

    chainID := chainConfig.ChainID
    nonce := uint64(0)
    value := big.NewInt(1000000000000)
    gasLimit := 21000
    gasPrice := big.NewInt(1000000000)
    bob, err := coreth.NewKey(rand.Reader); checkError(err)

    chain := coreth.NewETHChain(&config, nil)
    chain.SetOnSeal(func(block *types.Block) {
        go func() {
            time.Sleep(1000 * time.Millisecond)
            chain.GenBlock()
        }()
    })
    chain.Start()

    chain.GenBlock()
    for i := 0; i < 10; i++ {
        tx := types.NewTransaction(nonce, bob.Address, value, uint64(gasLimit), gasPrice, nil)
        signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey); checkError(err)
        _ = signedTx
        chain.AddRemoteTxs([]*types.Transaction{signedTx})
        time.Sleep(1000 * time.Millisecond)
        nonce++
    }

    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    signal.Notify(c, os.Interrupt, syscall.SIGINT)
    <-c
    chain.Stop()
}
