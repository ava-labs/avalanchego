package fastsyncer

import (
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

type Config struct {
	common.Config

	VM block.ChainVM
}
