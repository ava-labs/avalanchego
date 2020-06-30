package password

import "github.com/nbutton23/zxcvbn-go"

const (
	// maxCheckedPassLen limits the length of the password that should be
	// strength checked.
	//
	// As per issue https://github.com/ava-labs/gecko/issues/195 it was found
	// the longer the length of password the slower zxcvbn.PasswordStrength()
	// performs. To avoid performance issues, and a DoS vector, we only check
	// the first 50 characters of the password.
	maxCheckedPassLen = 50
)

// Strength is the strength of a password
type Strength int

// The scoring mechanism of the zxcvbn package is defined as follows:
// 0 # too guessable: risky password. (guesses < 10^3)
// 1 # very guessable: protection from throttled online attacks. (guesses < 10^6)
// 2 # somewhat guessable: protection from unthrottled online attacks. (guesses < 10^8)
// 3 # safely unguessable: moderate protection from offline slow-hash scenario. (guesses < 10^10)
// 4 # very unguessable: strong protection from offline slow-hash scenario. (guesses >= 10^10)
// Note: We could use the iota keyword to define each of the below consts, but I think in this case
// it's better to be explicit
const (
	// VeryWeak password
	VeryWeak = 0
	// Weak password
	Weak = 1
	// OK password
	OK = 2
	// Strong password
	Strong = 3
	// VeryStrong password
	VeryStrong = 4
)

// SufficientlyStrong returns true if [password] has strength
// greater than or equal to [desiredStrength]
func SufficientlyStrong(password string, desiredStrength Strength) bool {
	checkPass := password
	if len(password) > maxCheckedPassLen {
		checkPass = password[:maxCheckedPassLen]
	}
	if zxcvbn.PasswordStrength(checkPass, nil).Score < int(desiredStrength) {
		return false
	}
	return true
}
