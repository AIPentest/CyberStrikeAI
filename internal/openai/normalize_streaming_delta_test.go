package openai

import "testing"

func TestNormalizeStreamingDelta_RepeatedCharBoundary(t *testing.T) {
	// Streaming fragments at the boundary of repeated digits: must not "43" whose first character and "194" trailing characters wrongly merged.
	cur, d := normalizeStreamingDelta("https://x:194", "43")
	if want := "https://x:19443"; cur != want {
		t.Fatalf("next: want %q got %q", want, cur)
	}
	if d != "43" {
		t.Fatalf("delta: want %q got %q", "43", d)
	}
}

func TestNormalizeStreamingDelta_CumulativePrefix(t *testing.T) {
	cur, d := normalizeStreamingDelta("today", "today's weather")
	if cur != "today's weather" || d != "weather" {
		t.Fatalf("got cur=%q d=%q", cur, d)
	}
}

func TestNormalizeStreamingDelta_FullRetransmit(t *testing.T) {
	cur, d := normalizeStreamingDelta("today", "today")
	if d != "" || cur != "today" {
		t.Fatalf("got cur=%q d=%q", cur, d)
	}
}

func TestNormalizeStreamingDelta_SingleRuneRepeated(t *testing.T) {
	cur, d := normalizeStreamingDelta("ya", "ya")
	if want := "yaya"; cur != want {
		t.Fatalf("next: want %q got %q", want, cur)
	}
	if d != "ya" {
		t.Fatalf("delta: want %q got %q", "ya", d)
	}
	cur, d = normalizeStreamingDelta("4", "4")
	if want := "44"; cur != want {
		t.Fatalf("next: want %q got %q", want, cur)
	}
	if d != "4" {
		t.Fatalf("delta: want %q got %q", "4", d)
	}
}

func TestNormalizeStreamingDelta_CumulativeExtendsNumber(t *testing.T) {
	// buffered "194" then receives the cumulative string "19443" (Note "1943" is not "19443" the prefix; it cannot be tested from an erroneously written intermediate state HasPrefix).
	cur, d := normalizeStreamingDelta("194", "19443")
	if want := "19443"; cur != want {
		t.Fatalf("next: want %q got %q", want, cur)
	}
	if d != "43" {
		t.Fatalf("delta: want %q got %q", "43", d)
	}
}
