// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"errors"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	errNotSecp256Cred  = errors.New("expected secp256k1 credentials")
	errWrongOutputType = errors.New("wrong output type")
	ErrNotAliasGetter  = errors.New("state isn't msig alias getter")
	ErrMsigCombination = errors.New("msig combinations not supported")
)

type Owned interface {
	Owners() interface{}
}

type AliasGetter interface {
	GetMultisigAlias(ids.ShortID) (*multisig.AliasWithNonce, error)
}

type (
	RecoverMap map[ids.ShortID][secp256k1.SignatureLen]byte
)

type CaminoFx struct {
	Fx
}

func (fx *CaminoFx) Initialize(vmIntf interface{}) error {
	err := fx.Fx.Initialize(vmIntf)
	if err != nil {
		return err
	}

	c := fx.VM.CodecRegistry()
	if camino, ok := c.(codec.CaminoRegistry); ok {
		if err := camino.RegisterCustomType(&MultisigCredential{}); err != nil {
			return err
		}
	}
	return nil
}

func (fx *CaminoFx) VerifyPermission(txIntf, inIntf, credIntf, ownerIntf interface{}) error {
	if cred, ok := credIntf.(*MultisigCredential); ok {
		credIntf = &cred.Credential
	}
	return fx.Fx.VerifyPermission(txIntf, inIntf, credIntf, ownerIntf)
}

func (fx *CaminoFx) VerifyOperation(txIntf, opIntf, credIntf interface{}, utxosIntf []interface{}) error {
	if cred, ok := credIntf.(*MultisigCredential); ok {
		credIntf = &cred.Credential
	}
	return fx.Fx.VerifyOperation(txIntf, opIntf, credIntf, utxosIntf)
}

func (fx *CaminoFx) VerifyTransfer(txIntf, inIntf, credIntf, utxoIntf interface{}) error {
	if cred, ok := credIntf.(*MultisigCredential); ok {
		credIntf = &cred.Credential
	}
	return fx.Fx.VerifyTransfer(txIntf, inIntf, credIntf, utxoIntf)
}

func (*Fx) IsNestedMultisig(ownerIntf interface{}, msigIntf interface{}) (bool, error) {
	owner, ok := ownerIntf.(*OutputOwners)
	if !ok {
		return false, ErrWrongOwnerType
	}
	msig, ok := msigIntf.(AliasGetter)
	if !ok {
		return false, ErrNotAliasGetter
	}

	for _, addr := range owner.Addrs {
		_, err := msig.GetMultisigAlias(addr)
		switch {
		case err == nil:
			return true, nil
		case err != database.ErrNotFound:
			return false, err
		}
	}

	return false, nil
}

func (fx *Fx) RecoverAddresses(msg []byte, verifies []verify.Verifiable) (RecoverMap, error) {
	ret := make(RecoverMap, len(verifies))
	visited := make(map[[secp256k1.SignatureLen]byte]bool)

	txHash := hashing.ComputeHash256(msg)
	for _, v := range verifies {
		cred, ok := v.(CredentialIntf)
		if !ok {
			return nil, errNotSecp256Cred
		}
		for _, sig := range cred.Signatures() {
			if visited[sig] {
				continue
			}
			pk, err := fx.SECPFactory.RecoverHashPublicKey(txHash, sig[:])
			if err != nil {
				return nil, err
			}
			visited[sig] = true
			ret[pk.Address()] = sig
		}
	}
	return ret, nil
}

func (*Fx) VerifyMultisigOwner(outIntf, msigIntf interface{}) error {
	out, ok := outIntf.(Owned)
	if !ok {
		return errWrongOutputType
	}
	owners, ok := out.Owners().(*OutputOwners)
	if !ok {
		return ErrWrongOwnerType
	}
	msig, ok := msigIntf.(AliasGetter)
	if !ok {
		return ErrNotAliasGetter
	}

	// We don't support msig combinations / nesting for now
	if len(owners.Addrs) == 1 {
		alias, err := msig.GetMultisigAlias(owners.Addrs[0])
		if err == database.ErrNotFound {
			return nil
		} else if err != nil && err != database.ErrNotFound {
			return err
		}
		aliasOwners, ok := alias.Owners.(*OutputOwners)
		if !ok {
			return ErrWrongOwnerType
		}
		owners = aliasOwners
	}

	for _, addr := range owners.Addrs {
		if _, err := msig.GetMultisigAlias(addr); err != nil && err != database.ErrNotFound {
			return err
		} else if err == nil {
			return ErrMsigCombination
		}
	}

	return nil
}

func (fx *Fx) VerifyMultisigTransfer(txIntf, inIntf, credIntf, utxoIntf, msigIntf interface{}) error {
	tx, ok := txIntf.(UnsignedTx)
	if !ok {
		return ErrWrongTxType
	}
	in, ok := inIntf.(*TransferInput)
	if !ok {
		return ErrWrongInputType
	}
	cred, ok := credIntf.(CredentialIntf)
	if !ok {
		return ErrWrongCredentialType
	}
	out, ok := utxoIntf.(TransferOutputIntf)
	if !ok {
		return ErrWrongUTXOType
	}
	owners, ok := out.Owners().(*OutputOwners)
	if !ok {
		return ErrWrongOwnerType
	}
	msig, ok := msigIntf.(AliasGetter)
	if !ok {
		return ErrNotAliasGetter
	}

	if err := verify.All(out, in, cred); err != nil {
		return err
	} else if out.Amount() != in.Amt {
		return errors.New("out amount and input differ")
	}

	return fx.verifyMultisigCredentials(tx.Bytes(), &in.Input, cred, owners, msig)
}

func (fx *Fx) VerifyMultisigPermission(txIntf, inIntf, credIntf, ownerIntf, msigIntf interface{}) error {
	tx, ok := txIntf.(UnsignedTx)
	if !ok {
		return ErrWrongTxType
	}
	in, ok := inIntf.(*Input)
	if !ok {
		return ErrWrongInputType
	}
	cred, ok := credIntf.(CredentialIntf)
	if !ok {
		return ErrWrongCredentialType
	}
	owners, ok := ownerIntf.(*OutputOwners)
	if !ok {
		return ErrWrongUTXOType
	}

	msig, ok := msigIntf.(AliasGetter)
	if !ok {
		return ErrNotAliasGetter
	}

	if err := verify.All(owners, in, cred); err != nil {
		return err
	}

	return fx.verifyMultisigCredentials(tx.Bytes(), in, cred, owners, msig)
}

func (*Fx) CollectMultisigAliases(ownerIntf, msigIntf interface{}) ([]interface{}, error) {
	owners, ok := ownerIntf.(*OutputOwners)
	if !ok {
		return nil, ErrWrongUTXOType
	}
	msig, ok := msigIntf.(AliasGetter)
	if !ok {
		return nil, ErrNotAliasGetter
	}

	return collectMultisigAliases(owners, msig)
}

func (fx *Fx) VerifyMultisigMessage(msg []byte, inIntf, credIntf, ownerIntf, msigIntf interface{}) error {
	in, ok := inIntf.(*Input)
	if !ok {
		return ErrWrongInputType
	}
	cred, ok := credIntf.(CredentialIntf)
	if !ok {
		return ErrWrongCredentialType
	}
	owners, ok := ownerIntf.(*OutputOwners)
	if !ok {
		return ErrWrongUTXOType
	}

	msig, ok := msigIntf.(AliasGetter)
	if !ok {
		return ErrNotAliasGetter
	}

	if err := verify.All(owners, in, cred); err != nil {
		return err
	}

	return fx.verifyMultisigCredentials(msg, in, cred, owners, msig)
}

func (fx *Fx) verifyMultisigCredentials(msg []byte, in *Input, cred CredentialIntf, owners *OutputOwners, msig AliasGetter) error {
	sigIdxs := cred.SignatureIndices()
	if sigIdxs == nil {
		sigIdxs = in.SigIndices
	}

	if len(sigIdxs) != len(cred.Signatures()) {
		return ErrInputCredentialSignersMismatch
	}

	resolved, err := fx.RecoverAddresses(msg, []verify.Verifiable{cred})
	if err != nil {
		return err
	}

	tf := func(
		addr ids.ShortID,
		totalVisited,
		totalVerified uint32,
	) (bool, error) {
		// check that input sig index matches
		if totalVerified >= uint32(len(sigIdxs)) {
			return false, nil
		}

		if sig, exists := resolved[addr]; exists &&
			sig == cred.Signatures()[totalVerified] &&
			sigIdxs[totalVerified] == totalVisited {
			return true, nil
		}
		return false, nil
	}

	sigsVerified, err := TraverseOwners(owners, msig, tf)
	if err != nil {
		return err
	}
	if sigsVerified < uint32(len(sigIdxs)) {
		return ErrTooManySigners
	}

	return nil
}

func collectMultisigAliases(owners *OutputOwners, msig AliasGetter) ([]interface{}, error) {
	result := make([]interface{}, 0, len(owners.Addrs))

	tf := func(alias *multisig.AliasWithNonce) {
		result = append(result, alias)
	}

	if err := TraverseAliases(owners, msig, tf); err != nil {
		return nil, err
	}
	return result, nil
}

// ExtractFromAndSigners splits an array of PrivateKeys into `from` and `signers`
// The delimiter is a `nil` PrivateKey.
// If no delimiter exists, the given PrivateKeys are used for both from and signing
// Having different sets of `from` and `signer` allows multisignature feature
func ExtractFromAndSigners(keys []*secp256k1.PrivateKey) (set.Set[ids.ShortID], []*secp256k1.PrivateKey) {
	// find nil key which splits froms and signer
	splitIndex := len(keys)
	for index, key := range keys {
		if key == nil {
			splitIndex = index
			break
		}
	}

	if splitIndex == len(keys) {
		from := set.NewSet[ids.ShortID](len(keys))
		for _, key := range keys {
			from.Add(key.Address())
		}
		return from, keys
	}

	// Addresses we get UTXOs for
	from := set.NewSet[ids.ShortID](splitIndex)
	for index := 0; index < splitIndex; index++ {
		from.Add(keys[index].Address())
	}

	// Signers which will signe the Outputs
	signer := make([]*secp256k1.PrivateKey, len(keys)-splitIndex-1)
	for index := 0; index < len(signer); index++ {
		signer[index] = keys[index+splitIndex+1]
	}
	return from, signer
}
