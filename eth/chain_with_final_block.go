//nolint:unused
package eth

import (
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/subnet-evm/core"
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
