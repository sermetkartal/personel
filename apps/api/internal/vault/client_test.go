package vault

import "testing"

func TestParseKeyVersion(t *testing.T) {
	cases := []struct {
		in        string
		wantName  string
		wantVer   int
		wantError bool
	}{
		{"control-plane-signing:v1", "control-plane-signing", 1, false},
		{"control-plane-signing:v42", "control-plane-signing", 42, false},
		{"stub-key:v1", "stub-key", 1, false},
		// Key names with embedded colons — last colon wins.
		{"ns:inner:v3", "ns:inner", 3, false},
		// Malformed inputs must error, not silently parse.
		{"", "", 0, true},
		{"nokeyversion", "", 0, true},
		{"key:v", "", 0, true},
		{"key:vabc", "", 0, true},
		{"key:v0", "", 0, true}, // Vault versions are 1-indexed
		{"key:", "", 0, true},
		{":v1", "", 0, true},
	}
	for _, c := range cases {
		name, ver, err := parseKeyVersion(c.in)
		if c.wantError {
			if err == nil {
				t.Errorf("parseKeyVersion(%q): expected error, got (%q, %d, nil)", c.in, name, ver)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseKeyVersion(%q): unexpected error: %v", c.in, err)
			continue
		}
		if name != c.wantName || ver != c.wantVer {
			t.Errorf("parseKeyVersion(%q) = (%q, %d), want (%q, %d)",
				c.in, name, ver, c.wantName, c.wantVer)
		}
	}
}

func TestStubSignVerifyRoundtrip(t *testing.T) {
	// The stub client must satisfy the full Sign → Verify contract so
	// integration tests and in-process dev runs can exercise the
	// evidence chain without real Vault.
	c := NewStubClient()
	payload := []byte("hello evidence world")

	sig, keyVer, err := c.Sign(nil, payload)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if keyVer != "stub-key:v1" {
		t.Errorf("keyVer: %q", keyVer)
	}

	if err := c.Verify(nil, payload, sig, keyVer); err != nil {
		t.Errorf("Verify (roundtrip): %v", err)
	}

	// Tampering with the payload must fail verification.
	if err := c.Verify(nil, []byte("tampered"), sig, keyVer); err == nil {
		t.Error("Verify: expected error on tampered payload")
	}

	// Unknown key version must fail.
	if err := c.Verify(nil, payload, sig, "stub-key:v99"); err == nil {
		t.Error("Verify: expected error on unknown key version")
	}
}
