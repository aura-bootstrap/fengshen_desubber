package propainter

import (
	"testing"

	"github.com/aura-bootstrap/fengshen_desubber/internal/mask"
)

// fullMask marks every pixel of a w×h frame masked.
func fullMask(w, h int) mask.Frame {
	bits := make([]uint8, w*h)
	for i := range bits {
		bits[i] = 1
	}
	return mask.Encode(bits, w, h)
}

// A flicker outlier with agreeing neighbours collapses to the median.
func TestTemporalMedianSmoothsFlicker(t *testing.T) {
	w, h := 2, 1
	a := []byte{100, 100, 100, 50, 50, 50}
	b := []byte{160, 160, 160, 50, 50, 50} // pixel 0 flickers bright
	c := []byte{104, 104, 104, 50, 50, 50}
	frames := [][]byte{a, b, c}
	m := fullMask(w, h)
	temporalMedian(frames, []mask.Frame{m, m, m}, w, h)
	if got := b[0]; got != 104 {
		t.Fatalf("flicker pixel = %d, want median 104", got)
	}
	if got := b[3]; got != 50 {
		t.Fatalf("steady pixel = %d, want 50", got)
	}
}

// Real motion (neighbours disagree beyond the gate) must survive untouched.
func TestTemporalMedianKeepsMotion(t *testing.T) {
	w, h := 1, 1
	a := []byte{10, 0, 0}
	b := []byte{80, 0, 0}
	c := []byte{200, 0, 0}
	frames := [][]byte{a, b, c}
	m := fullMask(w, h)
	temporalMedian(frames, []mask.Frame{m, m, m}, w, h)
	if b[0] != 80 {
		t.Fatalf("moving pixel = %d, want 80", b[0])
	}
}

// Pixels outside the mask union are never touched.
func TestTemporalMedianRespectsMask(t *testing.T) {
	w, h := 2, 1
	a := []byte{100, 0, 0, 100, 0, 0}
	b := []byte{160, 0, 0, 160, 0, 0}
	c := []byte{104, 0, 0, 104, 0, 0}
	frames := [][]byte{a, b, c}
	bits := []uint8{1, 0} // only pixel 0 masked
	m := mask.Encode(bits, w, h)
	temporalMedian(frames, []mask.Frame{m, m, m}, w, h)
	if b[0] != 104 {
		t.Fatalf("masked pixel = %d, want 104", b[0])
	}
	if b[3] != 160 {
		t.Fatalf("unmasked pixel = %d, want 160", b[3])
	}
}

func chunksString(cs [][2]int) string {
	s := ""
	for _, c := range cs {
		s += "(" + itoa(c[0]) + "," + itoa(c[1]) + ")"
	}
	return s
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [8]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}

func assertChunks(t *testing.T, got [][2]int, want [][2]int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("chunks %s, want %s", chunksString(got), chunksString(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("chunks %s, want %s", chunksString(got), chunksString(want))
		}
	}
}

// Short ranges stay a single chunk.
func TestPlanChunksSingle(t *testing.T) {
	assertChunks(t, planChunks(0, 30, nil, 64, 12), [][2]int{{0, 30}})
	assertChunks(t, planChunks(5, 68, nil, 64, 12), [][2]int{{5, 68}})
}

// A 130-frame range splits into size-64 chunks overlapping by 12.
func TestPlanChunksOverlap(t *testing.T) {
	got := planChunks(0, 129, nil, 64, 12)
	assertChunks(t, got, [][2]int{{0, 63}, {52, 115}, {104, 129}})
	// Neighbours share exactly `overlap` frames.
	for i := 1; i < len(got); i++ {
		if ov := got[i-1][1] - got[i][0] + 1; ov != 12 {
			t.Fatalf("overlap %d between %v and %v", ov, got[i-1], got[i])
		}
	}
}

// Chunks never cross a cut; the chunk after a cut restarts without overlap.
func TestPlanChunksCutBreak(t *testing.T) {
	got := planChunks(0, 129, []int{40}, 64, 12)
	assertChunks(t, got, [][2]int{{0, 39}, {40, 103}, {92, 129}})
	for i := 1; i < len(got); i++ {
		if got[i][0] == 40 {
			if got[i-1][1] != 39 {
				t.Fatalf("cut chunk previous ends %d, want 39", got[i-1][1])
			}
			continue
		}
		if ov := got[i-1][1] - got[i][0] + 1; ov != 12 {
			t.Fatalf("overlap %d between %v and %v", ov, got[i-1], got[i])
		}
	}
}

// Multiple cuts each force a boundary.
func TestPlanChunksMultipleCuts(t *testing.T) {
	got := planChunks(0, 100, []int{20, 50, 90}, 64, 12)
	assertChunks(t, got, [][2]int{{0, 19}, {20, 49}, {50, 89}, {90, 100}})
}

// A cut just past a chunk boundary still truncates that chunk.
func TestPlanChunksCutAtEdge(t *testing.T) {
	got := planChunks(0, 70, []int{64}, 64, 12)
	assertChunks(t, got, [][2]int{{0, 63}, {64, 70}})
}

// Degenerate single-frame ranges terminate.
func TestPlanChunksTiny(t *testing.T) {
	assertChunks(t, planChunks(7, 7, nil, 64, 12), [][2]int{{7, 7}})
	assertChunks(t, planChunks(7, 8, []int{8}, 64, 12), [][2]int{{7, 7}, {8, 8}})
}

// Coverage is complete and ordered even with a cut inside the overlap zone.
func TestPlanChunksCoverage(t *testing.T) {
	for _, cuts := range [][]int{nil, {12}, {12, 60, 61, 90}} {
		cs := planChunks(3, 200, cuts, 64, 12)
		if cs[0][0] != 3 {
			t.Fatalf("start %d", cs[0][0])
		}
		prev := 3
		for _, c := range cs {
			if c[0] > prev {
				t.Fatalf("gap before %v (cuts %v)", c, cuts)
			}
			if c[1] < c[0] {
				t.Fatalf("empty chunk %v", c)
			}
			for _, cut := range cuts {
				if cut > c[0] && cut <= c[1] {
					t.Fatalf("chunk %v crosses cut %d", c, cut)
				}
			}
			prev = c[1] + 1
		}
		if cs[len(cs)-1][1] != 200 {
			t.Fatalf("end %d", cs[len(cs)-1][1])
		}
	}
}
