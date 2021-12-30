// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

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

type sortSnowballNodeData []*snowballNode

func (sb sortSnowballNodeData) Less(i, j int) bool {
	return bytes.Compare(sb[i].txID[:], sb[j].txID[:]) == -1
}
func (sb sortSnowballNodeData) Len() int      { return len(sb) }
func (sb sortSnowballNodeData) Swap(i, j int) { sb[j], sb[i] = sb[i], sb[j] }

func sortSnowballNodes(nodes []*snowballNode) {
	sort.Sort(sortSnowballNodeData(nodes))
}

// consensusString converts a list of snowball nodes into a human-readable
// string.
func consensusString(nodes []*snowballNode) string {
	// Sort the nodes so that the string representation is canonical
	sortSnowballNodes(nodes)

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
