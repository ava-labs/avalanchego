// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"fmt"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

var _ utils.Sortable[*snowballNode] = (*snowballNode)(nil)

type snowballNode struct {
	txID               ids.ID
	numSuccessfulPolls int
	confidence         int
}

func (sb *snowballNode) String() string {
	return fmt.Sprintf(
		"SB(NumSuccessfulPolls = %d, Confidence = %d)",
		sb.numSuccessfulPolls,
		sb.confidence)
}

func (sb *snowballNode) Less(other *snowballNode) bool {
	return sb.txID.Less(other.txID)
}

// consensusString converts a list of snowball nodes into a human-readable
// string.
func consensusString(nodes []*snowballNode) string {
	// Sort the nodes so that the string representation is canonical
	utils.Sort(nodes)

	sb := strings.Builder{}
	sb.WriteString("DG(")

	format := fmt.Sprintf(
		"\n    Choice[%s] = ID: %%50s %%s",
		formatting.IntFormat(len(nodes)-1))
	for i, txNode := range nodes {
		sb.WriteString(fmt.Sprintf(format, i, txNode.txID, txNode))
	}

	if len(nodes) > 0 {
		sb.WriteString("\n")
	}
	sb.WriteString(")")
	return sb.String()
}
