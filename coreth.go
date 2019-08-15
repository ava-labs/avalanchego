package coreth

import (
	"crypto/ecdsa"
	"io"
	"os"

	"github.com/Determinant/coreth/consensus/dummy"
	"github.com/Determinant/coreth/eth"
	"github.com/Determinant/coreth/node"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/mattn/go-isatty"
)

type Tx = types.Transaction
type Block = types.Block
type Hash = common.Hash

type ETHChain struct {
	backend *eth.Ethereum
	cb      *dummy.ConsensusCallbacks
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
	cb := new(dummy.ConsensusCallbacks)
	backend, _ := eth.New(&ctx, config, cb)
	chain := &ETHChain{backend: backend, cb: cb}
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

func (self *ETHChain) SetOnSeal(cb func(*types.Block)) {
	self.cb.OnSeal = cb
}

type Key struct {
	Address    common.Address
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

func init() {
	usecolor := (isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())) && os.Getenv("TERM") != "dumb"
	glogger := log.StreamHandler(io.Writer(os.Stderr), log.TerminalFormat(usecolor))
	log.Root().SetHandler(glogger)
}
