package main

import (
	"strings"
	"testing"
)

func TestRenderBrailleChart_Empty(t *testing.T) {
	if got := renderBrailleChart(nil, 40, 10); got != "" {
		t.Errorf("expected empty string for nil data, got %q", got)
	}
	if got := renderBrailleChart([]float64{1, 2, 3}, 2, 2); got != "" {
		t.Errorf("expected empty string for too-small dimensions, got %q", got)
	}
}

func TestRenderBrailleChart_GreenAndNonEmpty(t *testing.T) {
	data := make([]float64, 30)
	for i := range data {
		data[i] = float64(i % 10 * 10)
	}
	out := renderBrailleChart(data, 40, 10)
	if out == "" {
		t.Fatalf("expected non-empty rendered chart")
	}
	// The data series must be drawn in green (ANSI SGR 32).
	if !strings.Contains(out, "\x1b[32m") {
		t.Errorf("expected ANSI green escape (\\x1b[32m) in chart output")
	}
	// And the output must terminate with a reset to avoid bleeding styles.
	if !strings.HasSuffix(out, "\x1b[0m") {
		t.Errorf("expected chart output to end with ANSI reset")
	}
}
