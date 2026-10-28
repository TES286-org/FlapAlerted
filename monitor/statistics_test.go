package monitor

import (
	"FlapAlerted/bgp/session"
	"testing"
)

func TestBuildSessionSummaryList_AggregatesByLabel(t *testing.T) {
	rates := []session.SessionRate{
		{Label: "router-a", RateSec: 10},
		{Label: "router-a", RateSec: 5}, // duplicate label - should collapse
		{Label: "router-b", RateSec: 7},
	}

	got := buildSessionSummaryList(rates, 20)

	if len(got) != 2 {
		t.Fatalf("expected 2 datasets, got %d: %+v", len(got), got)
	}

	// Sorted by RateSec descending; router-a should win after aggregation
	if got[0].Label != "router-a" || got[0].RateSec != 15 {
		t.Errorf("expected router-a=15 first, got %+v", got[0])
	}
	if got[1].Label != "router-b" || got[1].RateSec != 7 {
		t.Errorf("expected router-b=7 second, got %+v", got[1])
	}
}

func TestBuildSessionSummaryList_OthersBucketWhenExceedingCap(t *testing.T) {
	// 6 distinct labels, cap of 3 -> top 3 kept plus "others"
	rates := []session.SessionRate{
		{Label: "a", RateSec: 6}, // rank 1
		{Label: "b", RateSec: 5}, // rank 2
		{Label: "c", RateSec: 4}, // rank 3
		{Label: "d", RateSec: 3},
		{Label: "e", RateSec: 2},
		{Label: "f", RateSec: 1},
	}

	got := buildSessionSummaryList(rates, 3)

	if len(got) != 4 {
		t.Fatalf("expected 4 entries (3 kept + others), got %d: %+v", len(got), got)
	}

	if got[0].Label != "a" || got[0].RateSec != 6 {
		t.Errorf("expected a=6 first, got %+v", got[0])
	}
	if got[1].Label != "b" || got[1].RateSec != 5 {
		t.Errorf("expected b=5 second, got %+v", got[1])
	}
	if got[2].Label != "c" || got[2].RateSec != 4 {
		t.Errorf("expected c=4 third, got %+v", got[2])
	}

	// 3 + 2 + 1 = 6
	if got[3].Label != "others" || got[3].RateSec != 6 {
		t.Errorf("expected others=6 last, got %+v", got[3])
	}
}

func TestBuildSessionSummaryList_EmptyInput(t *testing.T) {
	got := buildSessionSummaryList(nil, 20)
	if len(got) != 0 {
		t.Fatalf("expected empty result for nil input, got %+v", got)
	}

	got = buildSessionSummaryList([]session.SessionRate{}, 20)
	if len(got) != 0 {
		t.Fatalf("expected empty result for empty input, got %+v", got)
	}
}

func TestBuildSessionSummaryList_RespectsCapWhenBelowLimit(t *testing.T) {
	rates := []session.SessionRate{
		{Label: "x", RateSec: 1},
		{Label: "y", RateSec: 2},
	}

	got := buildSessionSummaryList(rates, 20)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries (below cap, no others bucket), got %d: %+v", len(got), got)
	}
	for _, s := range got {
		if s.Label == "others" {
			t.Errorf("did not expect an others bucket when below cap, got %+v", s)
		}
	}
}
