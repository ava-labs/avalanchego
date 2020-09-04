package password

import "testing"

type test struct {
	password string
	expected Strength
}

func TestSufficientlyStrong(t *testing.T) {
	tests := []test{
		{
			"",
			VeryWeak,
		},
		{
			"a",
			VeryWeak,
		},
		{
			"password",
			VeryWeak,
		},
		{
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
