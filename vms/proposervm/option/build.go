package option

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func Build(
	parentID ids.ID,
	coreBytes []byte,
) (Option, error) {
	block := option{
		PrntID:    parentID,
		coreBlock: coreBytes,
	}

	bytes, err := c.Marshal(version, &block)
	if err != nil {
		return nil, err
	}
	block.bytes = bytes

	block.id = hashing.ComputeHash256Array(block.bytes)
	return &block, nil
}
