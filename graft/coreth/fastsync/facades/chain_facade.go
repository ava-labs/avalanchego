package facades

import "github.com/ava-labs/coreth/chain"

type ChainFacade interface {
	LastAcceptedBlock() BlockFacade
	GetBlockByNumber(num uint64) BlockFacade
}

type EthChainFacade struct {
	ethChain *chain.ETHChain
}

func NewEthChainFacade(ethChain *chain.ETHChain) *EthChainFacade {
	return &EthChainFacade{ethChain: ethChain}
}

func (e EthChainFacade) LastAcceptedBlock() BlockFacade {
	return e.ethChain.LastAcceptedBlock()
}

func (e EthChainFacade) GetBlockByNumber(num uint64) BlockFacade {
	return e.ethChain.GetBlockByNumber(num)
}
