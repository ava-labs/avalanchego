module github.com/ava-labs/avalanchego

// Changes to the minimum golang version must also be replicated in
// scripts/ansible/roles/golang_base/defaults/main.yml
// scripts/build_avalanche.sh
// scripts/local.Dockerfile
// Dockerfile
// README.md
// go.mod (here, only major.minor can be specified)
go 1.16

require (
	github.com/Microsoft/go-winio v0.4.14
	github.com/NYTimes/gziphandler v1.1.1
	github.com/ava-labs/coreth v0.8.5-rc.2
	github.com/btcsuite/btcutil v1.0.2
	github.com/decred/dcrd/dcrec/secp256k1/v3 v3.0.0-20200627015759-01fd2de07837
	github.com/golang-jwt/jwt v3.2.1+incompatible
	github.com/golang/mock v1.3.1
	github.com/google/btree v1.0.1
	github.com/gorilla/handlers v1.4.2
	github.com/gorilla/mux v1.8.0
	github.com/gorilla/rpc v1.2.0
	github.com/gorilla/websocket v1.4.2
	github.com/hashicorp/go-hclog v0.14.1
	github.com/hashicorp/go-plugin v1.4.3
	github.com/holiman/bloomfilter/v2 v2.0.3
	github.com/huin/goupnp v1.0.2
	github.com/jackpal/gateway v1.0.6
	github.com/jackpal/go-nat-pmp v1.0.2
	github.com/kardianos/osext v0.0.0-20190222173326-2bc1f35cddc0
	github.com/linxGnu/grocksdb v1.6.34
	github.com/mitchellh/go-homedir v1.1.0
	github.com/mr-tron/base58 v1.2.0
	github.com/nbutton23/zxcvbn-go v0.0.0-20180912185939-ae427f1e4c1d
	github.com/prometheus/client_golang v1.7.1
	github.com/prometheus/client_model v0.2.0
	github.com/rs/cors v1.7.0
	github.com/spaolacci/murmur3 v1.1.0
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.7.1
	github.com/stretchr/testify v1.7.0
	github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7
	golang.org/x/crypto v0.0.0-20210322153248-0c34fe9e7dc2
	golang.org/x/net v0.0.0-20210805182204-aaa1db679c0d
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/time v0.0.0-20210723032227-1f47c861a9ac
	gonum.org/v1/gonum v0.9.1
	google.golang.org/grpc v1.37.0
	google.golang.org/protobuf v1.27.1
	gotest.tools v2.2.0+incompatible
)
