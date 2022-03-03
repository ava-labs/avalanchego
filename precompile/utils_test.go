package precompile

import (
	"testing"

	"gotest.tools/assert"
)

func TestFunctionSignatureRegex(t *testing.T) {
	type test struct {
		str  string
		pass bool
	}

	for _, test := range []test{
		{
			str:  "getBalance()",
			pass: true,
		},
		{
			str:  "getBalance(address)",
			pass: true,
		},
		{
			str:  "getBalance(address,address)",
			pass: true,
		},
		{
			str:  "getBalance(address,address,address)",
			pass: true,
		},
		{
			str:  "getBalance(address,address,address,uint256)",
			pass: true,
		},
		{
			str:  "getBalance(address,)",
			pass: false,
		},
		{
			str:  "getBalance(address,address,)",
			pass: false,
		},
		{
			str:  "getBalance(,)",
			pass: false,
		},
		{
			str:  "(address,)",
			pass: false,
		},
		{
			str:  "()",
			pass: false,
		},
		{
			str:  "dummy",
			pass: false,
		},
	} {
		assert.Equal(t, test.pass, functionSignatureRegex.MatchString(test.str), "unexpected result for %q", test.str)
	}
}
