package dummy

import (
    "fmt"
    "time"
    "errors"
    "runtime"
    "math/big"
    "golang.org/x/crypto/sha3"

    "github.com/ethereum/go-ethereum/consensus"
    "github.com/ethereum/go-ethereum/core/types"
    "github.com/ethereum/go-ethereum/core/state"
    "github.com/ethereum/go-ethereum/params"
    "github.com/ethereum/go-ethereum/common"
    "github.com/ethereum/go-ethereum/rlp"
    "github.com/ethereum/go-ethereum/rpc"
)

type ConsensusCallbacks struct {
    OnSeal func(*types.Block)
}

type DummyEngine struct {
    cb *ConsensusCallbacks
}

func NewDummyEngine(cb *ConsensusCallbacks) *DummyEngine {
    return &DummyEngine { cb: cb }
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
    return header.Coinbase, nil
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
    header.Difficulty = big.NewInt(1)
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
    if self.cb.OnSeal != nil {
        self.cb.OnSeal(block)
    }
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
    return big.NewInt(1)
}

func (self *DummyEngine) APIs(chain consensus.ChainReader) []rpc.API {
    return nil
}

func (self *DummyEngine) Close() error {
    return nil
}
