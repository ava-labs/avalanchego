package logging

import (
	"testing"
)

func TestAddFilePrefix(t *testing.T) {
	tests := []struct {
		desc     string
		config   Config
		prefixes []string
		want     string
	}{{
		desc:   "empty file prefix and nil prefixes case",
		config: Config{},
		want:   "",
	}, {
		desc:   "non empty prefix and nil prefixes case",
		config: Config{FileNamePrefix: "testing"},
		want:   "testing",
	}, {
		desc:     "non empty prefix and empty prefixes case",
		config:   Config{FileNamePrefix: "testing"},
		prefixes: []string{},
		want:     "testing",
	}, {
		desc:     "non empty prefix and single prefixes case",
		config:   Config{FileNamePrefix: "testing"},
		prefixes: []string{"one"},
		want:     "testing.one",
	}, {
		desc:     "non empty prefix and multi prefixes case",
		config:   Config{FileNamePrefix: "testing"},
		prefixes: []string{"one", "two"},
		want:     "testing.one.two",
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tt.config.AddFileNamePrefix(tt.prefixes...)
			if tt.config.FileNamePrefix != tt.want {
				t.Fatalf("build() failed: got %v, want %v", tt.config.FileNamePrefix, tt.want)
			}
		})
	}
}
