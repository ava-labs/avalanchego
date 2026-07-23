// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pgp

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha1"
	"crypto/x509"
	"encoding/asn1"
	"encoding/binary"
	"fmt"
	"math/big"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
	protecdsa "github.com/ProtonMail/go-crypto/openpgp/ecdsa"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// Signer is the signing backend abstraction.
//
// Contract change: Sign and PK return raw DER (ASN.1) bytes. Earlier versions
// returned base64-encoded DER; that encoding was dropped deliberately because
// AWS KMS (via the SDK) already returns raw bytes, making the round-trip
// pointless. The method signatures are unchanged, so an implementation written
// against the old contract still compiles — it must stop base64-encoding its
// Sign/PK outputs and return raw DER instead.
type Signer interface {
	// Sign takes a SHA-256 digest and produces a raw DER-encoded ECDSA signature.
	Sign(digest []byte) ([]byte, error)

	// PK returns the raw DER-encoded public key used to verify signatures produced by this Signer.
	PK() ([]byte, error)
}

// maxUserIDLen is the largest User ID that fits in an old-format User ID
// packet's single length octet.
const maxUserIDLen = 255

// ValidateUserID reports whether userID fits in an OpenPGP User ID packet.
func ValidateUserID(userID string) error {
	if len(userID) > maxUserIDLen {
		return fmt.Errorf("user ID too long: %d bytes (max %d)", len(userID), maxUserIDLen)
	}
	return nil
}

// GenerateConfig builds a Config from a Signer, a user ID, and the key's
// creation time. creationTime is stamped into the OpenPGP public key packet and
// therefore fixes the key's fingerprint; pass the KMS key's CreationDate (not
// time.Now) so the derived PGP key identity is stable and reproducible.
func GenerateConfig(signer Signer, user string, creationTime time.Time) (Config, error) {
	pk, err := signer.PK()
	if err != nil {
		return Config{}, fmt.Errorf("get public key: %w", err)
	}

	pgpPK, err := EncodePGPPK(pk, user, creationTime, signer)
	if err != nil {
		return Config{}, fmt.Errorf("encode PGP public key: %w", err)
	}

	return Config{Signer: signer, PGPPK: pgpPK}, nil
}

type Config struct {
	// PGPPK is an ASCII-armored OpenPGP public key block (.asc format) containing a single public key
	// used to verify signatures produced by the Signer.
	PGPPK []byte
	// Signer produces signatures over digests (SHA2-256) that can be verified with the public key in PGPPK.
	Signer Signer
}

type BinarySigner struct {
	Config Config
}

// Sign produces an ASCII-armored detached OpenPGP signature over the given binary data,
// using the Signer and PGPPK in the Config.
// The signature is suitable for verification with GPG or any OpenPGP-compliant tool.
func (b *BinarySigner) Sign(binaryToSign []byte) ([]byte, error) {
	// Extract the creation time and fingerprint from the OpenPGP public key
	pgpPK, err := parsePGPPublicKey(b.Config.PGPPK)
	if err != nil {
		return nil, fmt.Errorf("parse PGP public key: %w", err)
	}

	gpgPK, ok := pgpPK.PublicKey.(*protecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("expected ECDSA public key, got %T", pgpPK.PublicKey)
	}
	if name := gpgPK.GetCurve().GetCurveName(); name != "P-256" {
		return nil, fmt.Errorf("expected NIST P-256 key, got curve %q", name)
	}

	ecPK := &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     gpgPK.X,
		Y:     gpgPK.Y,
	}

	creationTime := uint32(pgpPK.CreationTime.Unix())
	fingerprint := computeFingerprint(ecPK, creationTime)
	keyID := computeKeyId(fingerprint)

	// Build hashed subpackets
	var hashedSubpackets bytes.Buffer
	writeSubpacket(&hashedSubpackets, 33, append([]byte{0x04}, fingerprint...))
	sigCreationTime := make([]byte, 4)
	binary.BigEndian.PutUint32(sigCreationTime, uint32(time.Now().Unix()))
	writeSubpacket(&hashedSubpackets, 2, sigCreationTime)

	hashSuffix, hsLen := buildHashSuffix(0x00, hashedSubpackets.Bytes()) // sig type: binary document

	// Build the raw data to be signed: message + hash suffix
	var dataToSign bytes.Buffer
	dataToSign.Write(binaryToSign)
	dataToSign.Write(hashSuffix)

	// Compute the hash tag (first 2 bytes of SHA-256 digest) for the signature packet
	h := crypto.SHA256.New()
	h.Write(dataToSign.Bytes())
	digest := h.Sum(nil)

	rawSig, err := b.Config.Signer.Sign(digest)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}

	if !ecdsa.VerifyASN1(ecPK, digest, rawSig) {
		return nil, fmt.Errorf("signature verification failed")
	}

	var ecdsaSig ECDSASig
	if _, err := asn1.Unmarshal(rawSig, &ecdsaSig); err != nil {
		return nil, fmt.Errorf("unmarshal signature: %w", err)
	}

	// Build unhashed subpackets
	var unhashedSubpackets bytes.Buffer
	keyIDBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(keyIDBytes, keyID)
	writeSubpacket(&unhashedSubpackets, 16, keyIDBytes)

	sigPkt := buildSignaturePacket(hashSuffix, hsLen, unhashedSubpackets, digest, ecdsaSig)

	var out bytes.Buffer
	w, err := armor.Encode(&out, "PGP SIGNATURE", nil)
	if err != nil {
		return nil, fmt.Errorf("armor encode: %w", err)
	}
	if _, err := w.Write(sigPkt.Bytes()); err != nil {
		return nil, fmt.Errorf("write signature: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close armor: %w", err)
	}

	return out.Bytes(), nil
}

func buildSignaturePacket(hashSuffix []byte, hsLen int, unhashedSubpackets bytes.Buffer, digest []byte, ecdsaSig ECDSASig) bytes.Buffer {
	var sigBody bytes.Buffer
	sigBody.Write(hashSuffix[:6+hsLen])
	uhLen := unhashedSubpackets.Len()
	sigBody.Write([]byte{byte(uhLen >> 8), byte(uhLen)})
	sigBody.Write(unhashedSubpackets.Bytes())
	sigBody.Write(digest[:2])
	sigBody.Write(mpiEncode(ecdsaSig.R))
	sigBody.Write(mpiEncode(ecdsaSig.S))

	// Wrap in packet header and armor
	var sigPkt bytes.Buffer
	bodyLen := sigBody.Len()
	if bodyLen < 192 {
		sigPkt.Write([]byte{0xC2, byte(bodyLen)})
	} else {
		sigPkt.Write([]byte{0xC2, 0xFF, byte(bodyLen >> 24), byte(bodyLen >> 16), byte(bodyLen >> 8), byte(bodyLen)})
	}
	sigPkt.Write(sigBody.Bytes())
	return sigPkt
}

func parsePublicKey(der []byte) (*ecdsa.PublicKey, error) {
	obj, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}

	pk, ok := obj.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("expected ECDSA public key, got %T", obj)
	}
	if pk.Curve != elliptic.P256() {
		return nil, fmt.Errorf("expected NIST P-256 key, got curve %q", pk.Curve.Params().Name)
	}

	return pk, nil
}

func parsePGPPublicKey(ascKey []byte) (*packet.PublicKey, error) {
	block, err := armor.Decode(bytes.NewReader(ascKey))
	if err != nil {
		return nil, fmt.Errorf("decode armor: %w", err)
	}

	reader := packet.NewReader(block.Body)
	p, err := reader.Next()
	if err != nil {
		return nil, fmt.Errorf("read packet: %w", err)
	}

	pk, ok := p.(*packet.PublicKey)
	if !ok {
		return nil, fmt.Errorf("expected public key packet, got %T", p)
	}

	return pk, nil
}

type ECDSASig struct {
	R, S *big.Int
}

// EncodePGPPK takes a raw DER-encoded ECDSA P-256 public key, a user ID string,
// the key's creation time, and a Signer to produce a self-signed ASCII-armored
// OpenPGP public key (.asc format) suitable for use with GPG. creationTime is
// hashed into the public key packet and thus determines the key's fingerprint.
func EncodePGPPK(pubKeyDER []byte, userID string, creationTime time.Time, signer Signer) ([]byte, error) {
	pk, err := parsePublicKey(pubKeyDER)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}

	if err := ValidateUserID(userID); err != nil {
		return nil, err
	}
	uidBytes := []byte(userID)

	creationTimeSecs := uint32(creationTime.Unix())
	pkBody := buildPublicKeyBody(pk, creationTimeSecs)
	fingerprint := computeFingerprint(pk, creationTimeSecs)

	// 1. Public key packet (tag 6, old-format header 0x99)
	var pkPkt bytes.Buffer
	pkPkt.Write([]byte{0x99, byte(len(pkBody) >> 8), byte(len(pkBody))})
	pkPkt.Write(pkBody)

	// 2. User ID packet (tag 13, old-format header 0xB4)
	var uidPkt bytes.Buffer
	uidPkt.WriteByte(0xB4)
	uidPkt.WriteByte(byte(len(uidBytes)))
	uidPkt.Write(uidBytes)

	// 3. Self-signature packet
	sigPkt, err := buildSelfSignature(pk, pkBody, uidBytes, fingerprint, creationTimeSecs, signer)
	if err != nil {
		return nil, fmt.Errorf("build self-signature: %w", err)
	}

	// Armor encode all packets
	var out bytes.Buffer
	w, err := armor.Encode(&out, "PGP PUBLIC KEY BLOCK", nil)
	if err != nil {
		return nil, fmt.Errorf("armor encode: %w", err)
	}
	if _, err := w.Write(pkPkt.Bytes()); err != nil {
		return nil, fmt.Errorf("write pk packet: %w", err)
	}
	if _, err := w.Write(uidPkt.Bytes()); err != nil {
		return nil, fmt.Errorf("write uid packet: %w", err)
	}
	if _, err := w.Write(sigPkt); err != nil {
		return nil, fmt.Errorf("write sig packet: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close armor: %w", err)
	}

	return out.Bytes(), nil
}

func buildSelfSignature(pk *ecdsa.PublicKey, pkBody, userID, fingerprint []byte, creationTime uint32, signer Signer) ([]byte, error) {
	keyID := computeKeyId(fingerprint)

	// Build hashed subpackets
	var hashedSubpackets bytes.Buffer

	// Subpacket 33: issuer fingerprint (1 byte version + 20 bytes fingerprint)
	writeSubpacket(&hashedSubpackets, 33, append([]byte{0x04}, fingerprint...))

	// Subpacket 2: signature creation time (4 bytes)
	creationTimeBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(creationTimeBytes, creationTime)
	writeSubpacket(&hashedSubpackets, 2, creationTimeBytes)

	// Subpacket 27: key flags (sign + certify)
	writeSubpacket(&hashedSubpackets, 27, []byte{0x03}) // certify (0x01) | sign (0x02)

	hashSuffix, hsLen := buildHashSuffix(0x13, hashedSubpackets.Bytes()) // sig type: positive certification

	// Build the raw data to be signed: public key + user ID + hash suffix
	var dataToSign bytes.Buffer
	dataToSign.Write([]byte{0x99, byte(len(pkBody) >> 8), byte(len(pkBody))})
	dataToSign.Write(pkBody)
	dataToSign.Write([]byte{0xB4, byte(len(userID) >> 24), byte(len(userID) >> 16), byte(len(userID) >> 8), byte(len(userID))})
	dataToSign.Write(userID)
	dataToSign.Write(hashSuffix)

	// Compute the hash tag (first 2 bytes of SHA-256 digest)
	h := crypto.SHA256.New()
	h.Write(dataToSign.Bytes())
	digest := h.Sum(nil)

	rawSig, err := signer.Sign(digest)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}

	if !ecdsa.VerifyASN1(pk, digest, rawSig) {
		return nil, fmt.Errorf("self-signature verification failed")
	}

	// Parse ASN1 signature into R, S
	var ecdsaSig ECDSASig
	if _, err := asn1.Unmarshal(rawSig, &ecdsaSig); err != nil {
		return nil, fmt.Errorf("unmarshal signature: %w", err)
	}

	// Build unhashed subpackets
	var unhashedSubpackets bytes.Buffer
	keyIDBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(keyIDBytes, keyID)
	writeSubpacket(&unhashedSubpackets, 16, keyIDBytes)

	// Build the full signature packet body
	var sigBody bytes.Buffer
	sigBody.Write(hashSuffix[:6+hsLen])
	// Unhashed subpackets
	uhLen := unhashedSubpackets.Len()
	sigBody.Write([]byte{byte(uhLen >> 8), byte(uhLen)})
	sigBody.Write(unhashedSubpackets.Bytes())
	// Hash tag (first 2 bytes of digest)
	sigBody.Write(digest[:2])
	// R and S MPIs
	sigBody.Write(mpiEncode(ecdsaSig.R))
	sigBody.Write(mpiEncode(ecdsaSig.S))

	// Wrap in packet header (tag 2, new-format)
	var pkt bytes.Buffer
	bodyLen := sigBody.Len()
	if bodyLen < 192 {
		pkt.Write([]byte{0xC2, byte(bodyLen)})
	} else {
		pkt.Write([]byte{0xC2, 0xFF, byte(bodyLen >> 24), byte(bodyLen >> 16), byte(bodyLen >> 8), byte(bodyLen)})
	}
	pkt.Write(sigBody.Bytes())

	return pkt.Bytes(), nil
}

// buildHashSuffix constructs an OpenPGP v4 HashSuffix from a signature type
// and serialized hashed subpackets. Returns the full HashSuffix bytes and
// the hashed subpackets length (needed to extract the hashed portion for serialization).
func buildHashSuffix(sigType byte, hashedSubpackets []byte) ([]byte, int) {
	hsLen := len(hashedSubpackets)
	var buf bytes.Buffer
	buf.WriteByte(0x04)    // version 4
	buf.WriteByte(sigType) // signature type
	buf.WriteByte(19)      // pub key algo: ECDSA
	buf.WriteByte(8)       // hash algo: SHA-256
	buf.Write([]byte{byte(hsLen >> 8), byte(hsLen)})
	buf.Write(hashedSubpackets)
	// Trailer
	totalLen := 6 + hsLen
	buf.Write([]byte{0x04, 0xff})
	buf.Write([]byte{byte(totalLen >> 24), byte(totalLen >> 16), byte(totalLen >> 8), byte(totalLen)})
	return buf.Bytes(), hsLen
}

func writeSubpacket(buf *bytes.Buffer, spType byte, content []byte) {
	spLen := 1 + len(content) // type byte + content
	if spLen < 192 {
		buf.WriteByte(byte(spLen))
	} else {
		buf.WriteByte(0xFF)
		buf.Write([]byte{byte(spLen >> 24), byte(spLen >> 16), byte(spLen >> 8), byte(spLen)})
	}
	buf.WriteByte(spType)
	buf.Write(content)
}

func mpiEncode(n *big.Int) []byte {
	bitLen := uint16(n.BitLen())
	b := n.Bytes()
	buf := make([]byte, 2+len(b))
	buf[0] = byte(bitLen >> 8)
	buf[1] = byte(bitLen)
	copy(buf[2:], b)
	return buf
}

func buildPublicKeyBody(pk *ecdsa.PublicKey, creationTime uint32) []byte {
	// OID for NIST P-256: 1.2.840.10045.3.1.7
	oid := []byte{0x2A, 0x86, 0x48, 0xCE, 0x3D, 0x03, 0x01, 0x07}
	oidEncoded := append([]byte{byte(len(oid))}, oid...)

	// MPI-encoded uncompressed public point
	point := elliptic.Marshal(pk.Curve, pk.X, pk.Y)
	bitLen := uint16(new(big.Int).SetBytes(point).BitLen())
	mpiEncoded := append([]byte{byte(bitLen >> 8), byte(bitLen)}, point...)

	body := &bytes.Buffer{}
	body.WriteByte(0x04) // version 4
	body.Write([]byte{byte(creationTime >> 24), byte(creationTime >> 16), byte(creationTime >> 8), byte(creationTime)})
	body.WriteByte(19) // algorithm: ECDSA
	body.Write(oidEncoded)
	body.Write(mpiEncoded)

	return body.Bytes()
}

func computeFingerprint(pk *ecdsa.PublicKey, creationTime uint32) []byte {
	body := buildPublicKeyBody(pk, creationTime)
	h := sha1.New()
	bodyLen := len(body)
	h.Write([]byte{0x99, byte(bodyLen >> 8), byte(bodyLen)})
	h.Write(body)
	return h.Sum(nil)
}

func computeKeyId(fingerprint []byte) uint64 {
	return binary.BigEndian.Uint64(fingerprint[12:20])
}
