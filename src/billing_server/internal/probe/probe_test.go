package probe

import (
	"path/filepath"
	"testing"

	"fengshen-desubber/billing_server/internal/billing"
	"fengshen-desubber/billing_server/internal/testutil"
)

func TestDurationSeconds(t *testing.T) {
	p := filepath.Join(t.TempDir(), "v.mp4")
	testutil.WriteTestMP4(t, p, 65)
	got, err := DurationSeconds(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != 65 {
		t.Fatalf("want 65, got %d", got)
	}
	if c := billing.Cost(got); c != 2 {
		t.Fatalf("65s should cost 2 credits, got %d", c)
	}
}

func TestDurationSecondsInvalid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.mp4")
	testutil.WriteBadFile(t, p)
	if _, err := DurationSeconds(p); err == nil {
		t.Fatal("expect error for non-mp4")
	}
}
