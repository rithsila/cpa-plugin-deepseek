package deepseekcpa

import (
	"strings"
	"testing"
)

func TestValidateConfigAcceptsHostOwnedFields(t *testing.T) {
	// The host writes enabled/priority into the same YAML tree it passes us.
	raw := []byte("enabled: true\npriority: 7\n")
	if err := validateConfig(raw); err != nil {
		t.Fatalf("validateConfig() error = %v, want nil", err)
	}
}

func TestValidateConfigAcceptsEmptyConfig(t *testing.T) {
	if err := validateConfig(nil); err != nil {
		t.Fatalf("validateConfig(nil) error = %v, want nil", err)
	}
}

func TestValidateConfigRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name:    "malformed yaml",
			raw:     "models: [",
			wantErr: "config",
		},
		{
			name:    "not a mapping",
			raw:     "just-a-string",
			wantErr: "config",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateConfig([]byte(tc.raw))
			if err == nil {
				t.Fatalf("validateConfig() error = nil, want error mentioning %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), tc.wantErr)
			}
		})
	}
}
