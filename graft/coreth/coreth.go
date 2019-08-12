package coreth

import (
    "math/big"
    "fmt"
    "time"
    "errors"
    "runtime"
    "io"
    "crypto/ecdsa"
    "golang.org/x/crypto/sha3"

    "github.com/ethereum/go-ethereum/core/types"
    "github.com/ethereum/go-ethereum/common"
    //"github.com/ethereum/go-ethereum/eth"
    "github.com/ethereum/go-ethereum/event"
    "github.com/ethereum/go-ethereum/params"
    "github.com/ethereum/go-ethereum/consensus"
    "github.com/ethereum/go-ethereum/core/state"
    "github.com/Determinant/coreth/miner"
    "github.com/Determinant/coreth/eth"
    "github.com/ethereum/go-ethereum/rlp"
    "github.com/ethereum/go-ethereum/rpc"
    "github.com/Determinant/coreth/node"
    "github.com/ethereum/go-ethereum/crypto"
)

type Tx = types.Transaction
type Block = types.Block
type Hash = common.Hash

type ETHChain struct {
    mux *event.TypeMux
    backend *eth.Ethereum
    worker *miner.Worker
}

type DummyEngine struct {
}

var (
    allowedFutureBlockTime    = 15 * time.Second  // Max time from current time allowed for blocks, before they're considered future blocks
)

var (
    errZeroBlockTime     = errors.New("timestamp equals parent's")
)

// modified from consensus.go
func (self *DummyEngine) verifyHeader(chain consensus.ChainReader, header, parent *types.Header, uncle bool, seal bool) error {
    // Ensure that the header's extra-data section is of a reasonable size
    if uint64(len(header.Extra)) > params.MaximumExtraDataSize {
        return fmt.Errorf("extra-data too long: %d > %d", len(header.Extra), params.MaximumExtraDataSize)
    }
    // Verify the header's timestamp
    if !uncle {
        if header.Time > uint64(time.Now().Add(allowedFutureBlockTime).Unix()) {
            return consensus.ErrFutureBlock
        }
    }
    if header.Time <= parent.Time {
        return errZeroBlockTime
    }
    // Verify that the gas limit is <= 2^63-1
    cap := uint64(0x7fffffffffffffff)
    if header.GasLimit > cap {
        return fmt.Errorf("invalid gasLimit: have %v, max %v", header.GasLimit, cap)
    }
    // Verify that the gasUsed is <= gasLimit
    if header.GasUsed > header.GasLimit {
        return fmt.Errorf("invalid gasUsed: have %d, gasLimit %d", header.GasUsed, header.GasLimit)
    }

    // Verify that the gas limit remains within allowed bounds
    diff := int64(parent.GasLimit) - int64(header.GasLimit)
    if diff < 0 {
        diff *= -1
    }
    limit := parent.GasLimit / params.GasLimitBoundDivisor

    if uint64(diff) >= limit || header.GasLimit < params.MinGasLimit {
        return fmt.Errorf("invalid gas limit: have %d, want %d += %d", header.GasLimit, parent.GasLimit, limit)
    }
    // Verify that the block number is parent's +1
    if diff := new(big.Int).Sub(header.Number, parent.Number); diff.Cmp(big.NewInt(1)) != 0 {
        return consensus.ErrInvalidNumber
    }
    // Verify the engine specific seal securing the block
    if seal {
        if err := self.VerifySeal(chain, header); err != nil {
            return err
        }
    }
    return nil
}

func (self *DummyEngine) verifyHeaderWorker(chain consensus.ChainReader, headers []*types.Header, seals []bool, index int) error {
    var parent *types.Header
    if index == 0 {
        parent = chain.GetHeader(headers[0].ParentHash, headers[0].Number.Uint64()-1)
    } else if headers[index-1].Hash() == headers[index].ParentHash {
        parent = headers[index-1]
    }
    if parent == nil {
        return consensus.ErrUnknownAncestor
    }
    if chain.GetHeader(headers[index].Hash(), headers[index].Number.Uint64()) != nil {
        return nil // known block
    }
    return self.verifyHeader(chain, headers[index], parent, false, seals[index])
}

func (self *DummyEngine) Author(header *types.Header) (common.Address, error) {
    addr := common.Address{}
    return addr, nil
}

func (self *DummyEngine) VerifyHeader(chain consensus.ChainReader, header *types.Header, seal bool) error {
    // Short circuit if the header is known, or it's parent not
    number := header.Number.Uint64()
    if chain.GetHeader(header.Hash(), number) != nil {
        return nil
    }
    parent := chain.GetHeader(header.ParentHash, number-1)
    if parent == nil {
        return consensus.ErrUnknownAncestor
    }
    // Sanity checks passed, do a proper verification
    return self.verifyHeader(chain, header, parent, false, seal)
}

func (self *DummyEngine) VerifyHeaders(chain consensus.ChainReader, headers []*types.Header, seals []bool) (chan<- struct{}, <-chan error) {
    // Spawn as many workers as allowed threads
    workers := runtime.GOMAXPROCS(0)
    if len(headers) < workers {
        workers = len(headers)
    }

    // Create a task channel and spawn the verifiers
    var (
        inputs = make(chan int)
        done   = make(chan int, workers)
        errors = make([]error, len(headers))
        abort  = make(chan struct{})
    )
    for i := 0; i < workers; i++ {
        go func() {
            for index := range inputs {
                errors[index] = self.verifyHeaderWorker(chain, headers, seals, index)
                done <- index
            }
        }()
    }

    errorsOut := make(chan error, len(headers))
    go func() {
        defer close(inputs)
        var (
            in, out = 0, 0
            checked = make([]bool, len(headers))
            inputs  = inputs
        )
        for {
            select {
            case inputs <- in:
                if in++; in == len(headers) {
                    // Reached end of headers. Stop sending to workers.
                    inputs = nil
                }
            case index := <-done:
                for checked[index] = true; checked[out]; out++ {
                    errorsOut <- errors[out]
                    if out == len(headers)-1 {
                        return
                    }
                }
            case <-abort:
                return
            }
        }
    }()
    return abort, errorsOut
}

func (self *DummyEngine) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
    return nil
}

func (self *DummyEngine) VerifySeal(chain consensus.ChainReader, header *types.Header) error {
    return nil
}

func (self *DummyEngine) Prepare(chain consensus.ChainReader, header *types.Header) error {
    header.Difficulty = big.NewInt(0)
    return nil
}

func (self *DummyEngine) Finalize(chain consensus.ChainReader, header *types.Header, state *state.StateDB, txs []*types.Transaction,
uncles []*types.Header) {
    // commit the final state root
    header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))
}

func (self *DummyEngine) FinalizeAndAssemble(chain consensus.ChainReader, header *types.Header, state *state.StateDB, txs []*types.Transaction,
uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
    // commit the final state root
    header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))

    // Header seems complete, assemble into a block and return
    return types.NewBlock(header, txs, uncles, receipts), nil
}

func (self *DummyEngine) Seal(chain consensus.ChainReader, block *types.Block, results chan<- *types.Block, stop <-chan struct{}) error {
    time.Sleep(1000 * time.Millisecond)
    fmt.Printf("sealed %s\n", block.ParentHash().String())
    results <- block
    return nil
}

func (self *DummyEngine) SealHash(header *types.Header) (hash common.Hash) {
    hasher := sha3.NewLegacyKeccak256()

    rlp.Encode(hasher, []interface{}{
        header.ParentHash,
        header.UncleHash,
        header.Coinbase,
        header.Root,
        header.TxHash,
        header.ReceiptHash,
        header.Bloom,
        header.Difficulty,
        header.Number,
        header.GasLimit,
        header.GasUsed,
        header.Time,
        header.Extra,
    })
    hasher.Sum(hash[:0])
    return hash
}

func (self *DummyEngine) CalcDifficulty(chain consensus.ChainReader, time uint64, parent *types.Header) *big.Int {
    return big.NewInt(0)
}

func (self *DummyEngine) APIs(chain consensus.ChainReader) []rpc.API {
    return nil
}

func (self *DummyEngine) Close() error {
    return nil
}

func isLocalBlock(block *types.Block) bool {
    return false
}

func NewETHChain(config *eth.Config, chainConfig *params.ChainConfig, etherBase *common.Address) *ETHChain {
    if config == nil {
        config = &eth.DefaultConfig
    }
    if chainConfig == nil {
        chainConfig = params.MainnetChainConfig
    }
    mux := new(event.TypeMux)
    ctx := node.NewServiceContext(mux)
    backend, _ := eth.New(&ctx, config)
    chain := &ETHChain {
        mux: mux,
        backend: backend,
        worker: miner.NewWorker(&config.Miner, chainConfig, &DummyEngine{}, backend, mux, isLocalBlock),
    }
    if etherBase == nil {
        etherBase = &common.Address{
            1, 0, 0, 0, 0, 0, 0, 0, 0, 0,
            0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
        }
    }
    chain.worker.SetEtherbase(*etherBase)
    return chain
}

func (self *ETHChain) Start() {
    self.worker.Start()
}

func (self *ETHChain) Stop() {
    self.worker.Stop()
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
