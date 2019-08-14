package coreth

import (
    "io"
    "crypto/ecdsa"

    "github.com/ethereum/go-ethereum/core/types"
    "github.com/ethereum/go-ethereum/common"
    //"github.com/ethereum/go-ethereum/eth"
    "github.com/ethereum/go-ethereum/event"
    "github.com/Determinant/coreth/eth"
    "github.com/Determinant/coreth/node"
    "github.com/ethereum/go-ethereum/crypto"
)

type Tx = types.Transaction
type Block = types.Block
type Hash = common.Hash

type ETHChain struct {
    backend *eth.Ethereum
}



func isLocalBlock(block *types.Block) bool {
    return false
}

func NewETHChain(config *eth.Config, etherBase *common.Address) *ETHChain {
    if config == nil {
        config = &eth.DefaultConfig
    }
    mux := new(event.TypeMux)
    ctx := node.NewServiceContext(mux)
    backend, _ := eth.New(&ctx, config)
    chain := &ETHChain { backend: backend }
    if etherBase == nil {
        etherBase = &common.Address{
            1, 0, 0, 0, 0, 0, 0, 0, 0, 0,
            0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
        }
    }
    backend.SetEtherbase(*etherBase)
    return chain
}

func (self *ETHChain) Start() {
    self.backend.StartMining(0)
}

func (self *ETHChain) Stop() {
    self.backend.StopPart()
}

func (self *ETHChain) GenBlock() {
    self.backend.Miner().GenBlock()
}

func (self *ETHChain) AddRemoteTxs(txs []*types.Transaction) []error {
    return self.backend.TxPool().AddRemotes(txs)
}

func (self *ETHChain) AddLocalTxs(txs []*types.Transaction) []error {
    return self.backend.TxPool().AddLocals(txs)
}

type Key struct {
	Address common.Address
	PrivateKey *ecdsa.PrivateKey
}

func NewKeyFromECDSA(privateKeyECDSA *ecdsa.PrivateKey) *Key {
    key := &Key{
        Address:    crypto.PubkeyToAddress(privateKeyECDSA.PublicKey),
        PrivateKey: privateKeyECDSA,
    }
    return key
}

func NewKey(rand io.Reader) (*Key, error) {
    privateKeyECDSA, err := ecdsa.GenerateKey(crypto.S256(), rand)
    if err != nil {
        return nil, err
    }
    return NewKeyFromECDSA(privateKeyECDSA), nil
}
