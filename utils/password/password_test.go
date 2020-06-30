package password

import "testing"

type test struct {
	password string
	expected Strength
}

func TestSufficientlyStrong(t *testing.T) {
	tests := []test{
		test{
			"",
			VeryWeak,
		},
		test{
			"a",
			VeryWeak,
		},
		test{
			"password",
			VeryWeak,
		},
		test{
			"thisisareallylongandpresumablyverystrongpassword",
			VeryStrong,
		},
	}

	for _, tt := range tests {
		if !SufficientlyStrong(tt.password, tt.expected) {
			t.Fatalf("expected %s to be rated stronger", tt.password)
		}
	}
}
