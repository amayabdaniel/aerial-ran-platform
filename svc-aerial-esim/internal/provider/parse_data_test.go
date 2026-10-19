package provider

import "testing"

// parseDataMB previously discarded its parse error and returned 0 for everything
// it could not handle, so a real "0 GB", a missing space ("5GB"), a TB plan, an
// unknown unit, "unlimited" and "" all produced the same 0 — a silent wrong
// quantity feeding billing/provisioning. These assert each is now distinct.
func TestParseDataMB(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"5 GB", 5120, false},
		{"500 MB", 500, false},
		{"5GB", 5120, false},     // no space
		{"500MB", 500, false},    // no space
		{"1 TB", 1024 * 1024, false},
		{"1.5 GB", 1536, false},
		{"10 KB", 0, false},      // rounds down to 0 MB but parses cleanly
		{"0 GB", 0, false},       // a REAL zero — must NOT be an error
		{"unlimited", DataUnlimited, false},
		{"Unlimited", DataUnlimited, false},
		{"  5 GB  ", 5120, false}, // surrounding space trimmed
		{"", 0, true},
		{"1 TB extra", 0, true},
		{"5 Gigabytes", 0, true}, // unknown unit
		{"abc", 0, true},
		{"GB", 0, true}, // no number
	}
	for _, c := range cases {
		got, err := parseDataMB(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseDataMB(%q): want error, got %d with nil error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseDataMB(%q): unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseDataMB(%q) = %d, want %d", c.in, got, c.want)
		}
	}

	// The crux of the defect: a genuine zero and a parse failure must not share
	// a representation.
	z, zErr := parseDataMB("0 GB")
	if zErr != nil || z != 0 {
		t.Fatalf("'0 GB' must be a clean zero, got %d err=%v", z, zErr)
	}
	if _, fErr := parseDataMB("0 Gigabytes"); fErr == nil {
		t.Fatal("a parse failure must return an error, not be indistinguishable from 0")
	}
}
