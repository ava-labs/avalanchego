// // tests/e2e/connect/suite_test.go
// package connect_test
//
// import (
//
//	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
//	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
//	. "github.com/onsi/ginkgo/v2"
//
// )
//
// var _ = SynchronizedBeforeSuite(
//
//	// This runs _once_, on the first Ginkgo node:
//	func() []byte {
//		flagVars := e2e.RegisterFlagsWithDefaultOwner("avalanchego-e2e")
//		desired := tmpnet.NewDefaultNetwork("connectrpc-health-api")
//
//		env := e2e.NewTestEnvironment(
//			e2e.NewEventHandlerTestContext(),
//			flagVars,
//			desired,
//		)
//		return env.Marshal()
//	},
//	// This runs on _every_ Ginkgo node:
//	func(envBytes []byte) {
//		tc := e2e.NewTestContext()
//		e2e.InitSharedTestEnvironment(tc, envBytes)
//	},
//
// )
// tests/connect/info_suite_test.go
package connect_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestInfoServiceConnectRPC(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "InfoService ConnectRPC Suite")
}
