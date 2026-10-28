package table

import (
	"FlapAlerted/bgp/common"
	"FlapAlerted/config"
	"net/netip"
	"testing"
)

func init() {
	// Provide a sane import limit so unit tests that exercise the
	// PrefixTable do not trip the import-limit guard.
	config.GlobalConf = config.UserConfig{ImportLimit: 1000}
}

func testPrefixV4(cidr string) netip.Prefix {
	p := netip.MustParsePrefix(cidr)
	return p
}

func testAsPath(asns ...uint32) common.AsPath {
	return common.AsPath(asns)
}

// TestPrefixTable_SwapPathChangeCount verifies the per-session update
// counter is incremented exactly once for each PathChange the PrefixTable
// emits, and that SwapPathChangeCount atomically resets the counter so
// subsequent reads return only the activity accumulated since the last
// swap.
func TestPrefixTable_SwapPathChangeCount(t *testing.T) {
	pathChangeChan := make(chan PathChange, 100)
	tbl := NewPrefixTable(pathChangeChan, nil)

	// Empty table should report zero activity.
	if got := tbl.SwapPathChangeCount(); got != 0 {
		t.Fatalf("expected zero count on fresh table, got %d", got)
	}

	// Trigger a path change by announcing a known prefix twice (same pathID)
	// with a different AS path. The second update must produce a single
	// PathChange.
	asPath1 := testAsPath(1)
	asPath2 := testAsPath(2)

	tbl.update(testPrefixV4("10.0.0.0/24"), 1, false, asPath1)
	// First announcement only creates an entry - no PathChange
	if got := tbl.SwapPathChangeCount(); got != 0 {
		t.Fatalf("expected 0 PathChange for initial announcement, got %d", got)
	}

	tbl.update(testPrefixV4("10.0.0.0/24"), 1, false, asPath2)
	// Second announcement with different path -> PathChange emitted
	if got := tbl.SwapPathChangeCount(); got != 1 {
		t.Fatalf("expected 1 PathChange after path replacement, got %d", got)
	}

	// Reading again should be zero (counter was swapped).
	if got := tbl.SwapPathChangeCount(); got != 0 {
		t.Fatalf("expected 0 PathChange after swap, got %d", got)
	}

	// Withdrawal must also produce a PathChange.
	tbl.update(testPrefixV4("10.0.0.0/24"), 1, true, nil)
	if got := tbl.SwapPathChangeCount(); got != 1 {
		t.Fatalf("expected 1 PathChange after withdrawal, got %d", got)
	}

	// Drain channel so the goroutine/chan doesn't keep us from returning.
	for len(pathChangeChan) > 0 {
		<-pathChangeChan
	}
}

// TestPrefixTable_LastIntervalCount verifies that LastIntervalCount returns
// the value from the most recent Swap without consuming the live counter.
// This is the contract relied on by HTTP handlers (/sessions, Prometheus)
// that must not interfere with the statistics collector.
func TestPrefixTable_LastIntervalCount(t *testing.T) {
	pathChangeChan := make(chan PathChange, 100)
	tbl := NewPrefixTable(pathChangeChan, nil)

	// Before any swap, the last-interval snapshot is zero.
	if got := tbl.LastIntervalCount(); got != 0 {
		t.Fatalf("expected 0 before first swap, got %d", got)
	}

	// Produce two path changes and swap once.
	tbl.update(testPrefixV4("10.0.0.0/24"), 1, false, testAsPath(1))
	tbl.update(testPrefixV4("10.0.0.0/24"), 1, false, testAsPath(2))
	tbl.update(testPrefixV4("10.0.0.0/24"), 1, false, testAsPath(3))
	swapped := tbl.SwapPathChangeCount()
	if swapped != 2 {
		t.Fatalf("expected 2 from swap, got %d", swapped)
	}

	// LastIntervalCount must mirror the swapped value without resetting.
	if got := tbl.LastIntervalCount(); got != 2 {
		t.Fatalf("expected LastIntervalCount=2, got %d", got)
	}

	// Repeated non-destructive reads must be stable.
	if got := tbl.LastIntervalCount(); got != 2 {
		t.Fatalf("expected stable LastIntervalCount=2, got %d", got)
	}

	// A new swap with no activity since must report 0 but preserve the
	// previous interval's snapshot until the next swap overwrites it.
	tbl.SwapPathChangeCount()
	if got := tbl.LastIntervalCount(); got != 0 {
		t.Fatalf("expected LastIntervalCount=0 after empty swap, got %d", got)
	}

	for len(pathChangeChan) > 0 {
		<-pathChangeChan
	}
}
