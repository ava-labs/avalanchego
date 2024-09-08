// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

const (
	defaultSubnetDirName = "subnets"
	jsonFileSuffix       = ".json"
)

type Chain struct {
	// Set statically
	VMID    ids.ID
	Config  string
	Genesis []byte
	// VersionArgs are the argument(s) to pass to the VM binary to receive
	// version details in json format (e.g. `--version-json`). This
	// supports checking that the rpcchainvm version of the VM binary
	// matches the version used by the configured avalanchego binary. If
	// empty, the version check will be skipped.
	VersionArgs []string

	// Set at runtime
	ChainID      ids.ID
	PreFundedKey *secp256k1.PrivateKey
}

// Write the chain configuration to the specified directory.
func (c *Chain) WriteConfig(chainDir string) error {
	// TODO(marun) Ensure removal of an existing file if no configuration should be provided
	if len(c.Config) == 0 {
		return nil
	}

	chainConfigDir := filepath.Join(chainDir, c.ChainID.String())
	if err := os.MkdirAll(chainConfigDir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create chain config dir: %w", err)
	}

	path := filepath.Join(chainConfigDir, defaultConfigFilename)
	if err := os.WriteFile(path, []byte(c.Config), perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write chain config: %w", err)
	}

	return nil
}

type Subnet struct {
	// A unique string that can be used to refer to the subnet across different temporary
	// networks (since the SubnetID will be different every time the subnet is created)
	Name string

	Config FlagsMap

	// The ID of the transaction that created the subnet
	SubnetID ids.ID

	// The private key that owns the subnet
	OwningKey *secp256k1.PrivateKey

	// IDs of the nodes responsible for validating the subnet
	ValidatorIDs []ids.NodeID

	Chains []*Chain
}

// Retrieves a wallet configured for use with the subnet
func (s *Subnet) GetWallet(ctx context.Context, uri string) (primary.Wallet, error) {
	keychain := secp256k1fx.NewKeychain(s.OwningKey)

	// Only fetch the subnet transaction if a subnet ID is present. This won't be true when
	// the wallet is first used to create the subnet.
	subnetIDs := []ids.ID{}
	if s.SubnetID != ids.Empty {
		subnetIDs = append(subnetIDs, s.SubnetID)
	}

	return primary.MakeWallet(ctx, &primary.WalletConfig{
		URI:          uri,
		AVAXKeychain: keychain,
		EthKeychain:  keychain,
		SubnetIDs:    subnetIDs,
	})
}

// Issues the subnet creation transaction and retains the result. The URI of a node is
// required to issue the transaction.
func (s *Subnet) Create(ctx context.Context, uri string) error {
	wallet, err := s.GetWallet(ctx, uri)
	if err != nil {
		return err
	}
	pWallet := wallet.P()

	subnetTx, err := pWallet.IssueCreateSubnetTx(
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				s.OwningKey.Address(),
			},
		},
		common.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to create subnet %s: %w", s.Name, err)
	}
	s.SubnetID = subnetTx.ID()

	return nil
}

func (s *Subnet) CreateChains(ctx context.Context, w io.Writer, uri string) error {
	wallet, err := s.GetWallet(ctx, uri)
	if err != nil {
		return err
	}
	pWallet := wallet.P()

	if _, err := fmt.Fprintf(w, "Creating chains for subnet %q\n", s.Name); err != nil {
		return err
	}

	for _, chain := range s.Chains {
		createChainTx, err := pWallet.IssueCreateChainTx(
			s.SubnetID,
			chain.Genesis,
			chain.VMID,
			nil,
			"",
			common.WithContext(ctx),
		)
		if err != nil {
			return fmt.Errorf("failed to create chain: %w", err)
		}
		chain.ChainID = createChainTx.ID()

		if _, err := fmt.Fprintf(w, " created chain %q for VM %q on subnet %q\n", chain.ChainID, chain.VMID, s.Name); err != nil {
			return err
		}
	}
	return nil
}

// Add validators to the subnet
func (s *Subnet) AddValidators(ctx context.Context, w io.Writer, apiURI string, nodes ...*Node) error {
	wallet, err := s.GetWallet(ctx, apiURI)
	if err != nil {
		return err
	}
	pWallet := wallet.P()

	// Collect the end times for current validators to reuse for subnet validators
	pvmClient := platformvm.NewClient(apiURI)
	validators, err := pvmClient.GetCurrentValidators(ctx, constants.PrimaryNetworkID, nil)
	if err != nil {
		return err
	}
	endTimes := make(map[ids.NodeID]uint64)
	for _, validator := range validators {
		endTimes[validator.NodeID] = validator.EndTime
	}

	startTime := time.Now().Add(DefaultValidatorStartTimeDiff)
	for _, node := range nodes {
		endTime, ok := endTimes[node.NodeID]
		if !ok {
			return fmt.Errorf("failed to find end time for %s", node.NodeID)
		}

		_, err := pWallet.IssueAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: node.NodeID,
					Start:  uint64(startTime.Unix()),
					End:    endTime,
					Wght:   units.Schmeckle,
				},
				Subnet: s.SubnetID,
			},
			common.WithContext(ctx),
		)
		if err != nil {
			return err
		}

		if _, err := fmt.Fprintf(w, " added %s as validator for subnet `%s`\n", node.NodeID, s.Name); err != nil {
			return err
		}
	}

	return nil
}

// Write the subnet configuration to disk
func (s *Subnet) Write(subnetDir string, chainDir string) error {
	if err := os.MkdirAll(subnetDir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create subnet dir: %w", err)
	}
	tmpnetConfigPath := filepath.Join(subnetDir, s.Name+jsonFileSuffix)

	// Since subnets are expected to be serialized for the first time
	// without their chains having been created (i.e. chains will have
	// empty IDs), use the absence of chain IDs as a prompt for a
	// subnet name uniqueness check.
	if len(s.Chains) > 0 && s.Chains[0].ChainID == ids.Empty {
		_, err := os.Stat(tmpnetConfigPath)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil {
			return fmt.Errorf("a subnet with name %s already exists", s.Name)
		}
	}

	// Write subnet configuration for tmpnet
	bytes, err := DefaultJSONMarshal(s)
	if err != nil {
		return fmt.Errorf("failed to marshal tmpnet subnet %s: %w", s.Name, err)
	}
	if err := os.WriteFile(tmpnetConfigPath, bytes, perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write tmpnet subnet config %s: %w", s.Name, err)
	}

	// The subnet and chain configurations for avalanchego can only be written once
	// they have been created since the id of the creating transaction must be
	// included in the path.
	if s.SubnetID == ids.Empty {
		return nil
	}

	// TODO(marun) Ensure removal of an existing file if no configuration should be provided
	if len(s.Config) > 0 {
		// Write subnet configuration for avalanchego
		bytes, err = DefaultJSONMarshal(s.Config)
		if err != nil {
			return fmt.Errorf("failed to marshal avalanchego subnet config %s: %w", s.Name, err)
		}

		subnetConfigPath := filepath.Join(subnetDir, s.SubnetID.String()+jsonFileSuffix)
		if err := os.WriteFile(subnetConfigPath, bytes, perms.ReadWrite); err != nil {
			return fmt.Errorf("failed to write avalanchego subnet config %s: %w", s.Name, err)
		}
	}

	for _, chain := range s.Chains {
		if err := chain.WriteConfig(chainDir); err != nil {
			return err
		}
	}

	return nil
}

// HasChainConfig indicates whether at least one of the subnet's
// chains have explicit configuration. This can be used to determine
// whether validator restart is required after chain creation to
// ensure that chains are configured correctly.
func (s *Subnet) HasChainConfig() bool {
	for _, chain := range s.Chains {
		if len(chain.Config) > 0 {
			return true
		}
	}
	return false
}

func WaitForActiveValidators(
	ctx context.Context,
	w io.Writer,
	pChainClient platformvm.Client,
	subnet *Subnet,
) error {
	ticker := time.NewTicker(DefaultPollingInterval)
	defer ticker.Stop()

	if _, err := fmt.Fprintf(w, "Waiting for validators of subnet %q to become active\n", subnet.Name); err != nil {
		return err
	}

	if _, err := fmt.Fprint(w, " "); err != nil {
		return err
	}

	for {
		if _, err := fmt.Fprint(w, "."); err != nil {
			return err
		}
		validators, err := pChainClient.GetCurrentValidators(ctx, subnet.SubnetID, nil)
		if err != nil {
			return err
		}
		validatorSet := set.NewSet[ids.NodeID](len(validators))
		for _, validator := range validators {
			validatorSet.Add(validator.NodeID)
		}
		allActive := true
		for _, validatorID := range subnet.ValidatorIDs {
			if !validatorSet.Contains(validatorID) {
				allActive = false
			}
		}
		if allActive {
			if _, err := fmt.Fprintf(w, "\n saw the expected active validators of subnet %q\n", subnet.Name); err != nil {
				return err
			}
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to see the expected active validators of subnet %q before timeout", subnet.Name)
		case <-ticker.C:
		}
	}
}

// Reads subnets from [network dir]/subnets/[subnet name].json
func readSubnets(subnetDir string) ([]*Subnet, error) {
	entries, err := os.ReadDir(subnetDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to read subnet dir: %w", err)
	}

	subnets := []*Subnet{}
	for _, entry := range entries {
		if entry.IsDir() {
			// Looking only for files
			continue
		}
		fileName := entry.Name()
		if filepath.Ext(fileName) != jsonFileSuffix {
			// Subnet files should have a .json extension
			continue
		}
		fileNameWithoutSuffix := strings.TrimSuffix(fileName, jsonFileSuffix)
		// Skip actual subnet config files, which are named [subnetID].json
		if _, err := ids.FromString(fileNameWithoutSuffix); err == nil {
			// Skip files that are named by their SubnetID
			continue
		}

		subnetPath := filepath.Join(subnetDir, fileName)
		bytes, err := os.ReadFile(subnetPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read subnet file %s: %w", subnetPath, err)
		}
		subnet := &Subnet{}
		if err := json.Unmarshal(bytes, subnet); err != nil {
			return nil, fmt.Errorf("failed to unmarshal subnet from %s: %w", subnetPath, err)
		}
		subnets = append(subnets, subnet)
	}

	return subnets, nil
}
