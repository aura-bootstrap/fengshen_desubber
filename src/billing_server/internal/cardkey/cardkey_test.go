package cardkey

import (
	"strings"
	"testing"
)

func TestGenerateFormat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		code, err := Generate()
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(code, "-")
		if len(parts) != 5 {
			t.Fatalf("groups: %s", code)
		}
		for _, p := range parts {
			if len(p) != 5 {
				t.Fatalf("group len: %s", code)
			}
			for _, ch := range p {
				if !strings.ContainsRune(Alphabet, ch) {
					t.Fatalf("bad char %c in %s", ch, code)
				}
			}
		}
		if seen[code] {
			t.Fatalf("duplicate: %s", code)
		}
		seen[code] = true
	}
}

func TestNormalizeAndHash(t *testing.T) {
	code := "abcde-fghjk-mnpqr-stuvw-xyz23"
	if Normalize(code) != "ABCDEFGHJKMNPQRSTUVWXYZ23" {
		t.Fatalf("normalize: %s", Normalize(code))
	}
	h2a, _ := HashV2("pepper", code)
	h2b, _ := HashV2("pepper", strings.ToLower(strings.ReplaceAll(code, "-", " ")))
	if h2a != h2b {
		t.Fatalf("v2 hash unstable under normalization")
	}
	if len(h2a) != 64 {
		t.Fatalf("hash len: %d", len(h2a))
	}
	if _, err := HashV2("", code); err != ErrNoPepper {
		t.Fatalf("empty pepper must fail: %v", err)
	}
	if HashV1(code) == h2a {
		t.Fatalf("v1/v2 must differ")
	}
	cands, _ := CandidateHashes("pepper", code)
	if len(cands) != 2 || cands[0] != h2a || cands[1] != HashV1(code) {
		t.Fatalf("candidates: %v", cands)
	}
	if Mask(code) != "ABCDE…3" {
		t.Fatalf("mask: %s", Mask(code))
	}
}
