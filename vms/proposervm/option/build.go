package option

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func Build(
	parentID ids.ID,
	innerBytes []byte,
) (Option, error) {
	opt := option{
		PrntID:     parentID,
		InnerBytes: innerBytes,
	}

	bytes, err := c.Marshal(version, &opt)
	if err != nil {
		return nil, err
	}
	opt.bytes = bytes

	opt.id = hashing.ComputeHash256Array(opt.bytes)
	return &opt, nil
}
