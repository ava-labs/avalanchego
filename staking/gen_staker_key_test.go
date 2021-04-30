package staking

import (
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMakeKeys(t *testing.T) {
	certBytes, keyBytes, err := NewStakerKeys()
	assert.NoError(t, err)

	cert, err := tls.X509KeyPair(certBytes, keyBytes)
	assert.NoError(t, err)

	blockc, _ := pem.Decode(certBytes)
	x509cert, err := x509.ParseCertificate(blockc.Bytes)
	assert.NoError(t, err)

	msg := []byte(fmt.Sprintf("msg %d", time.Now().Unix()))

	msgHash := sha256.New()
	_, err = msgHash.Write(msg)
	assert.NoError(t, err)

	msgHashSum := msgHash.Sum(nil)

	sig, err := cert.PrivateKey.(crypto.Signer).Sign(rand.Reader, msgHashSum, crypto.SHA256)
	assert.NoError(t, err)

	err = x509cert.CheckSignature(x509cert.SignatureAlgorithm, msg, sig)
	assert.NoError(t, err)
}
