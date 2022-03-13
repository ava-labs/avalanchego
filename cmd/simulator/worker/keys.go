package worker

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

const (
	workerDir = ".simulator/keys"
)

// readASCII reads into 'buf', stopping when the buffer is full or
// when a non-printable control character is encountered.
func readASCII(buf []byte, r *bufio.Reader) (n int, err error) {
	for ; n < len(buf); n++ {
		buf[n], err = r.ReadByte()
		switch {
		case err == io.EOF || buf[n] < '!':
			return n, nil
		case err != nil:
			return n, err
		}
	}
	return n, nil
}

// checkKeyFileEnd skips over additional newlines at the end of a key file.
func checkKeyFileEnd(r *bufio.Reader) error {
	for i := 0; ; i++ {
		b, err := r.ReadByte()
		switch {
		case err == io.EOF:
			return nil
		case err != nil:
			return err
		case b != '\n' && b != '\r':
			return fmt.Errorf("invalid character %q at end of key file", b)
		case i >= 2:
			return errors.New("key file too long, want 64 hex characters")
		}
	}
}

func loadKey(file string) (*crypto.PrivateKeySECP256K1R, error) {
	fd, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer fd.Close()

	r := bufio.NewReader(fd)
	buf := make([]byte, 64)
	n, err := readASCII(buf, r)
	if err != nil {
		return nil, err
	} else if n != len(buf) {
		return nil, fmt.Errorf("key file too short, want 64 hex characters")
	}
	if err := checkKeyFileEnd(r); err != nil {
		return nil, err
	}

	pkBytes, err := hex.DecodeString(string(buf))
	if err != nil {
		return nil, err
	}
	factory := &crypto.FactorySECP256K1R{}
	pk, err := factory.ToPrivateKey(pkBytes)
	if err != nil {
		return nil, err
	}

	return pk.(*crypto.PrivateKeySECP256K1R), nil
}

func ewoq() *crypto.PrivateKeySECP256K1R {
	k := "PrivateKey-ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN"
	trimmedPrivateKey := strings.TrimPrefix(k, constants.SecretKeyPrefix)
	privKeyBytes, err := formatting.Decode(formatting.CB58, trimmedPrivateKey)
	if err != nil {
		panic(err)
	}

	factory := crypto.FactorySECP256K1R{}
	skIntf, err := factory.ToPrivateKey(privKeyBytes)
	if err != nil {
		panic(err)
	}
	return skIntf.(*crypto.PrivateKeySECP256K1R)

}

func LoadAvailableKeys(ctx context.Context) ([]*crypto.PrivateKeySECP256K1R, error) {
	if _, err := os.Stat(workerDir); os.IsNotExist(err) {
		if err := os.MkdirAll(workerDir, 0755); err != nil {
			return nil, fmt.Errorf("unable to create %s: %w", workerDir, err)
		}

		return nil, nil
	}

	var files []string

	err := filepath.Walk(workerDir, func(path string, info os.FileInfo, err error) error {
		if path == workerDir {
			return nil
		}

		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("could not walk %s: %w", workerDir, err)
	}

	pks := make([]*crypto.PrivateKeySECP256K1R, len(files))
	for i, file := range files {
		pk, err := loadKey(file)
		if err != nil {
			return nil, fmt.Errorf("could not load key at %s: %w", file, err)
		}

		pks[i] = pk
	}
	pks = append(pks, ewoq())

	return pks, nil
}

func SaveKey(key *crypto.PrivateKeySECP256K1R) error {
	k := hex.EncodeToString(key.Bytes())
	return ioutil.WriteFile(filepath.Join(workerDir, key.PublicKey().Address().Hex()), []byte(k), 0600)
}

func GenerateKey() (*crypto.PrivateKeySECP256K1R, error) {
	factory := &crypto.FactorySECP256K1R{}
	pk, err := factory.NewPrivateKey()
	if err != nil {
		return nil, err
	}

	return pk.(*crypto.PrivateKeySECP256K1R), nil
}
