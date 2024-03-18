package eth

import (
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
)

const blocksToKeep = 604_800 // Approx. 2 weeks worth of blocks assuming 2s block time

type chainWithFinalBlock struct {
	*core.BlockChain
}

// CurrentFinalBlock returns the current block below which blobs should not
// be maintained anymore for reorg purposes.
func (c *chainWithFinalBlock) CurrentFinalBlock() *types.Header {
	lastAccepted := c.LastAcceptedBlock().Header().Number.Uint64()
	if lastAccepted <= blocksToKeep {
		return nil
	}

	return c.GetHeaderByNumber(lastAccepted - blocksToKeep)
}
