package ids

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/ava-labs/avalanchego/utils"
)

const NodeIDPrefix = "NodeID-"

type NodeID ShortID

var EmptyNodeID = NodeID{}

func (id NodeID) String() string {
	return ShortID(id).PrefixedString(NodeIDPrefix)
}

func (id NodeID) Bytes() []byte {
	return id[:]
}

func (id NodeID) MarshalJSON() ([]byte, error) {
	return []byte("\"" + id.String() + "\""), nil
}

func (id *NodeID) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == nullStr { // If "null", do nothing
		return nil
	} else if len(str) <= 2+len(NodeIDPrefix) {
		return fmt.Errorf("expected NodeID length to be > %d", 2+len(NodeIDPrefix))
	}

	lastIndex := len(str) - 1
	if str[0] != '"' || str[lastIndex] != '"' {
		return errMissingQuotes
	}

	var err error
	*id, err = NodeIDFromString(str[1:lastIndex])
	return err
}

// ToNodeID attempt to convert a byte slice into a node id
func ToNodeID(bytes []byte) (NodeID, error) {
	nodeID, err := ToShortID(bytes)
	if err != nil {
		return NodeID{}, nil
	}
	return NodeID(nodeID), nil
}

type sortNodeIDData []NodeID

func (ids sortNodeIDData) Less(i, j int) bool {
	return bytes.Compare(
		ids[i].Bytes(),
		ids[j].Bytes()) == -1
}
func (ids sortNodeIDData) Len() int      { return len(ids) }
func (ids sortNodeIDData) Swap(i, j int) { ids[j], ids[i] = ids[i], ids[j] }

// SortNodeIDs sorts the node IDs lexicographically
func SortNodeIDs(nodeIDs []NodeID) {
	sort.Sort(sortNodeIDData(nodeIDs))
}

// IsSortedAndUniqueNodeIDs returns true if the ids are sorted and unique
func IsSortedAndUniqueNodeIDs(nodeIDs []NodeID) bool {
	return utils.IsSortedAndUnique(sortNodeIDData(nodeIDs))
}

// NodeIDFromString is the inverse of NodeID.String()
func NodeIDFromString(nodeIDStr string) (NodeID, error) {
	asShort, err := ShortFromPrefixedString(nodeIDStr, NodeIDPrefix)
	if err != nil {
		return NodeID{}, err
	}
	return NodeID(asShort), nil
}
