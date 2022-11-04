package evm

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ava-labs/avalanchego/version"
	"github.com/stretchr/testify/assert"
)

type rpcChainCompatibility struct {
	RPCChainVMProtocolVersion map[string]uint `json:"rpcChainVMProtocolVersion"`
}

const compatibilityFile = "../../compatibility.json"

func TestCompatibility(t *testing.T) {
	compat, err := os.ReadFile(compatibilityFile)
	assert.NoError(t, err)

	var parsedCompat rpcChainCompatibility
	err = json.Unmarshal(compat, &parsedCompat)
	assert.NoError(t, err)

	rpcChainVMVersion, valueInJSON := parsedCompat.RPCChainVMProtocolVersion[Version]
	assert.True(t, valueInJSON)
	assert.Equal(t, rpcChainVMVersion, version.RPCChainVMProtocol)
}
