// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package importtx

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/spf13/cobra"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"

	"github.com/ava-labs/xsvm/api"
	"github.com/ava-labs/xsvm/tx"
)

func Command() *cobra.Command {
	c := &cobra.Command{
		Use:   "import",
		Short: "Issues an import transaction",
		RunE:  importFunc,
	}
	flags := c.Flags()
	AddFlags(flags)
	return c
}

func importFunc(c *cobra.Command, args []string) error {
	flags := c.Flags()
	config, err := ParseFlags(flags, args)
	if err != nil {
		return err
	}

	ctx := c.Context()

	var (
		// Note: here we assume the unsigned message is correct from the last
		//       URI in sourceURIs. In practice this shouldn't be done.
		unsignedMessage *warp.UnsignedMessage
		// Note: assumes that sourceURIs are all of the validators of the subnet
		//       and that they do not share public keys.
		signatures = make([]*bls.Signature, len(config.SourceURIs))
	)
	for i, uri := range config.SourceURIs {
		xsClient := api.NewClient(uri, config.SourceChainID)

		fetchStartTime := time.Now()
		var rawSignature []byte
		unsignedMessage, rawSignature, err = xsClient.Message(ctx, config.TxID)
		if err != nil {
			return fmt.Errorf("failed to fetch BLS signature from %s with: %w", uri, err)
		}

		sig, err := bls.SignatureFromBytes(rawSignature)
		if err != nil {
			return fmt.Errorf("failed to parse BLS signature from %s with: %w", uri, err)
		}

		// Note: the public key should not be fetched from the node in practice.
		//       The public key should be fetched from the P-chain directly.
		infoClient := info.NewClient(uri)
		_, nodePOP, err := infoClient.GetNodeID(ctx)
		if err != nil {
			return fmt.Errorf("failed to fetch BLS public key from %s with: %w", uri, err)
		}

		pk := nodePOP.Key()
		if !bls.Verify(pk, sig, unsignedMessage.Bytes()) {
			return fmt.Errorf("failed to verify BLS signature against public key from %s", uri)
		}

		log.Printf("fetched BLS signature from %s in %s\n", uri, time.Since(fetchStartTime))
		signatures[i] = sig
	}

	signers := set.NewBits()
	for i := range signatures {
		signers.Add(i)
	}
	signature := &warp.BitSetSignature{
		Signers: signers.Bytes(),
	}

	aggSignature, err := bls.AggregateSignatures(signatures)
	if err != nil {
		return err
	}

	aggSignatureBytes := bls.SignatureToBytes(aggSignature)
	copy(signature.Signature[:], aggSignatureBytes)

	message, err := warp.NewMessage(
		unsignedMessage,
		signature,
	)
	if err != nil {
		return err
	}

	client := api.NewClient(config.URI, config.DestinationChainID)

	nonce, err := client.Nonce(ctx, config.PrivateKey.Address())
	if err != nil {
		return err
	}

	utx := &tx.Import{
		Nonce:   nonce,
		MaxFee:  config.MaxFee,
		Message: message.Bytes(),
	}
	stx, err := tx.Sign(utx, config.PrivateKey)
	if err != nil {
		return err
	}

	txJSON, err := json.MarshalIndent(stx, "", "  ")
	if err != nil {
		return err
	}

	issueTxStartTime := time.Now()
	txID, err := client.IssueTx(ctx, stx)
	if err != nil {
		return err
	}
	log.Printf("issued tx %s in %s\n%s\n", txID, time.Since(issueTxStartTime), string(txJSON))
	return nil
}
