package runner

import (
	"strings"
	"testing"
)

func collect(t *testing.T, input string) []Event {
	t.Helper()
	ch := make(chan Event, 512)
	parseStream(strings.NewReader(input), ch)
	close(ch)
	var out []Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func TestParseStagesAndProgress(t *testing.T) {
	in := "video 720x1280 25fps 4.1s; band y=742 h=538\n" +
		"scene cuts: 1 (0.2s)\n" +
		"detect: 103 frames, 83 with text (165 boxes), 2 events, 0.6s\n" +
		"repair 0/103\rrepair 51/103\rrepair 102/103\r\n" +
		"engine temporal (propainter x2): 123.8s -> data/out/x.mp4\n" +
		"verify: 32/103 frames still detected, 51 boxes, 0 overlapping original subtitle regions (0.5s)\n"
	evs := collect(t, in)
	var stages []string
	var lastDone, lastTotal int
	for _, ev := range evs {
		if ev.Type == "stage" {
			stages = append(stages, ev.Stage)
		}
		if ev.Type == "progress" {
			lastDone, lastTotal = ev.Done, ev.Total
		}
	}
	want := []string{"probe", "cuts", "detect", "engine", "verify"}
	if strings.Join(stages, ",") != strings.Join(want, ",") {
		t.Fatalf("stages %v, want %v", stages, want)
	}
	if lastDone != 102 || lastTotal != 103 {
		t.Fatalf("last progress %d/%d", lastDone, lastTotal)
	}
}

func TestParseTrailingJSONReport(t *testing.T) {
	in := "verify: done\n{\n  \"output\": \"x.mp4\",\n  \"elapsed_sec\": 1.5\n}\n"
	evs := collect(t, in)
	var report string
	for _, ev := range evs {
		if ev.Type == "report" {
			report = ev.Msg
		}
	}
	if !strings.Contains(report, `"elapsed_sec": 1.5`) {
		t.Fatalf("report missing: %q", report)
	}
}

func TestParamsArgs(t *testing.T) {
	p := Params{Propainter: true, Grain: true, OCR: true, CRF: 15, ForceEngine: "propainter"}
	got := strings.Join(p.args(), " ")
	want := "--propainter --grain --ocr --crf 15 --force-engine propainter"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	empty := Params{}
	if len(empty.args()) != 0 {
		t.Fatalf("empty params should yield no flags: %v", empty.args())
	}
}
