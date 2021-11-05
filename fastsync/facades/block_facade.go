package facades

import "github.com/ethereum/go-ethereum/common"

type BlockFacade interface {
	NumberU64() uint64
	Root() common.Hash
	ExtData() []byte
}
