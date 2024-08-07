package test

import (
	"crypto/rsa"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
)

func TestGetNodeKeysAndIDs(t *testing.T) {
	t.Skip()
	nodeKeys, nodeIDs := nodeKeysAndIDs("staking/local", 5)
	for i := range nodeKeys {
		fmt.Println(nodeKeys[i].String())
		fmt.Println(nodeIDs[i].String())
		fmt.Println()
	}
}

func nodeKeysAndIDs(certsAndKeysPath string, nodeCount int) ([]*secp256k1.PrivateKey, []ids.NodeID) {
	nodeKeys := make([]*secp256k1.PrivateKey, nodeCount)
	nodeIDs := make([]ids.NodeID, nodeCount)
	moduleRootDir, err := findRootDir("go.mod")
	if err != nil {
		panic(err)
	}
	certsAndKeysPath = filepath.Join(moduleRootDir, certsAndKeysPath)
	for i := 0; i < nodeCount; i++ {
		cert, err := staking.LoadTLSCertFromFiles(
			filepath.Join(certsAndKeysPath, fmt.Sprintf("staker%d.key", i+1)),
			filepath.Join(certsAndKeysPath, fmt.Sprintf("staker%d.crt", i+1)),
		)
		if err != nil {
			panic(err)
		}

		rsaKey, ok := cert.PrivateKey.(*rsa.PrivateKey)
		if !ok {
			panic("Wrong private key type")
		}
		secpPrivateKey := secp256k1.RsaPrivateKeyToSecp256PrivateKey(rsaKey)
		nodePrivateKey, err := factory.ToPrivateKey(secpPrivateKey.Serialize())
		if err != nil {
			panic(err)
		}
		nodeKeys[i] = nodePrivateKey
		nodeIDs[i] = ids.NodeID(nodePrivateKey.Address())
	}
	return nodeKeys, nodeIDs
}

func findRootDir(target string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, target)); err == nil {
			return dir, nil
		}
		parentDir := filepath.Dir(dir)
		if parentDir == dir {
			break // Reached the root of the filesystem
		}
		dir = parentDir
	}
	return "", fmt.Errorf("could not find root directory containing %s", target)
}
