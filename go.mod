module github.com/ava-labs/avalanchego

// Changes to the minimum golang version must also be replicated in
// scripts/build_avalanche.sh
// scripts/local.Dockerfile
// Dockerfile
// README.md
// go.mod (here, only major.minor can be specified)
go 1.19

require (
	github.com/Microsoft/go-winio v0.5.2
	github.com/NYTimes/gziphandler v1.1.1
	github.com/ava-labs/avalanche-network-runner-sdk v0.3.0
	github.com/ava-labs/coreth v0.11.9-rc.0
	github.com/ava-labs/ledger-avalanche/go v0.0.0-20230105152938-00a24d05a8c7
	github.com/btcsuite/btcd/btcutil v1.1.3
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.1.0
	github.com/golang-jwt/jwt/v4 v4.3.0
	github.com/golang/mock v1.6.0
	github.com/google/btree v1.1.2
	github.com/gorilla/mux v1.8.0
	github.com/gorilla/rpc v1.2.0
	github.com/gorilla/websocket v1.4.2
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0
	github.com/holiman/bloomfilter/v2 v2.0.3
	github.com/huin/goupnp v1.0.3
	github.com/jackpal/gateway v1.0.6
	github.com/jackpal/go-nat-pmp v1.0.2
	github.com/mr-tron/base58 v1.2.0
	github.com/nbutton23/zxcvbn-go v0.0.0-20180912185939-ae427f1e4c1d
	github.com/onsi/ginkgo/v2 v2.4.0
	github.com/onsi/gomega v1.24.0
	github.com/pires/go-proxyproto v0.6.2
	github.com/prometheus/client_golang v1.13.0
	github.com/prometheus/client_model v0.2.0
	github.com/rs/cors v1.7.0
	github.com/shirou/gopsutil v3.21.11+incompatible
	github.com/spaolacci/murmur3 v1.1.0
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.12.0
	github.com/stretchr/testify v1.8.1
	github.com/supranational/blst v0.3.11-0.20220920110316-f72618070295
	github.com/syndtr/goleveldb v1.0.1-0.20220614013038-64ee5596c38a
	go.opentelemetry.io/otel v1.11.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.11.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.11.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.11.0
	go.opentelemetry.io/otel/sdk v1.11.0
	go.opentelemetry.io/otel/trace v1.11.0
	go.uber.org/zap v1.24.0
	golang.org/x/crypto v0.0.0-20220622213112-05595931fe9d
	golang.org/x/exp v0.0.0-20220426173459-3bcf042a4bf5
	golang.org/x/sync v0.1.0
	golang.org/x/term v0.5.0
	golang.org/x/time v0.0.0-20210723032227-1f47c861a9ac
	gonum.org/v1/gonum v0.11.0
	google.golang.org/genproto v0.0.0-20221027153422-115e99e71e1c
	google.golang.org/grpc v1.50.1
	google.golang.org/protobuf v1.28.1
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
)

require (
	github.com/VictoriaMetrics/fastcache v1.10.0 // indirect
	github.com/benbjohnson/clock v1.3.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/btcsuite/btcd/btcec/v2 v2.3.2 // indirect
	github.com/cenkalti/backoff/v4 v4.1.3 // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/deckarep/golang-set v1.8.0 // indirect
	github.com/dlclark/regexp2 v1.4.1-0.20201116162257-a2a8dda75c91 // indirect
	github.com/dop251/goja v0.0.0-20220405120441-9037c2b61cbf // indirect
	github.com/ethereum/go-ethereum v1.10.26 // indirect
	github.com/fjl/memsize v0.0.0-20190710130421-bcb5799ab5e5 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/gballet/go-libpcsclite v0.0.0-20191108122812-4678299bea08 // indirect
	github.com/go-logr/logr v1.2.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/go-stack/stack v1.8.0 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/google/uuid v1.2.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.12.0 // indirect
	github.com/hashicorp/go-bexpr v0.1.10 // indirect
	github.com/hashicorp/golang-lru v0.5.5-0.20210104140557-80c98217689d // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/holiman/big v0.0.0-20221017200358-a027dc42d04e // indirect
	github.com/holiman/uint256 v1.2.0 // indirect
	github.com/magiconair/properties v1.8.6 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	github.com/mattn/go-runewidth v0.0.9 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2-0.20181231171920-c182affec369 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/mitchellh/pointerstructure v1.2.0 // indirect
	github.com/olekukonko/tablewriter v0.0.5 // indirect
	github.com/pelletier/go-toml v1.9.5 // indirect
	github.com/pelletier/go-toml/v2 v2.0.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/common v0.37.0 // indirect
	github.com/prometheus/procfs v0.8.0 // indirect
	github.com/rjeczalik/notify v0.9.3 // indirect
	github.com/spf13/afero v1.8.2 // indirect
	github.com/spf13/cast v1.5.0 // indirect
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/status-im/keycard-go v0.0.0-20200402102358-957c09536969 // indirect
	github.com/subosito/gotenv v1.3.0 // indirect
	github.com/tklauser/go-sysconf v0.3.5 // indirect
	github.com/tklauser/numcpus v0.2.2 // indirect
	github.com/tyler-smith/go-bip39 v1.0.2 // indirect
	github.com/yusufpapurcu/wmi v1.2.2 // indirect
	github.com/zondax/hid v0.9.1 // indirect
	github.com/zondax/ledger-go v0.14.1 // indirect
	go.opentelemetry.io/otel/exporters/otlp/internal/retry v1.11.0 // indirect
	go.opentelemetry.io/proto/otlp v0.19.0 // indirect
	go.uber.org/atomic v1.10.0 // indirect
	go.uber.org/multierr v1.8.0 // indirect
	golang.org/x/net v0.7.0 // indirect
	golang.org/x/sys v0.5.0 // indirect
	golang.org/x/text v0.7.0 // indirect
	gopkg.in/ini.v1 v1.66.4 // indirect
	gopkg.in/urfave/cli.v1 v1.20.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
