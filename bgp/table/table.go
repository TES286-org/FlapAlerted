package table

import (
	"FlapAlerted/bgp/common"
	"FlapAlerted/bgp/notification"
	"FlapAlerted/config"
	"context"
	"net/netip"
	"sync/atomic"
)

type PrefixTable struct {
	table          map[netip.Prefix]*Entry
	pathChangeChan chan PathChange
	importCount    atomic.Uint32
	// pathChangeCount tracks the total number of PathChange events emitted
	// by this session's table. It is swapped (atomically reset to zero) by
	// the statistics collector every collection interval so that the per-
	// session update rate can be derived independently for each peer.
	pathChangeCount atomic.Uint64
	// lastIntervalCount holds the count returned by the most recent
	// SwapPathChangeCount call. It allows non-destructive readers (e.g.
	// the /sessions endpoint or Prometheus exporter) to report the latest
	// interval's rate without consuming the live counter.
	lastIntervalCount   atomic.Uint64
	sessionCancellation context.CancelCauseFunc
}

func NewPrefixTable(pathChangeChan chan PathChange, sessionCancellation context.CancelCauseFunc) *PrefixTable {
	return &PrefixTable{table: make(map[netip.Prefix]*Entry), pathChangeChan: pathChangeChan, sessionCancellation: sessionCancellation}
}

type PathChange struct {
	Prefix       netip.Prefix
	IsWithdrawal bool
	IsSamePath   bool
	OldPath      common.AsPath
}

type Entry struct {
	Paths map[uint32]common.AsPath
}

func (t *PrefixTable) update(prefix netip.Prefix, pathID uint32, isWithdrawal bool, asPath common.AsPath) {
	if isWithdrawal {
		if entry, ok := t.table[prefix]; ok {
			if oldPath, exists := entry.Paths[pathID]; exists {
				t.pathChangeChan <- PathChange{
					Prefix:       prefix,
					IsWithdrawal: true,
					OldPath:      oldPath,
				}
				t.pathChangeCount.Add(1)
				t.importCount.Add(^uint32(0))
				delete(entry.Paths, pathID)
				if len(entry.Paths) == 0 {
					delete(t.table, prefix)
				}
			}
		}
	} else {
		entry, found := t.table[prefix]
		if !found {
			if t.importCount.Add(1) > config.GlobalConf.ImportLimit {
				t.importCount.Add(^uint32(0))
				t.sessionCancellation(notification.ErrImportLimit)
				return
			}
			entry = &Entry{Paths: make(map[uint32]common.AsPath)}
			t.table[prefix] = entry
		} else {
			if oldPath, existed := entry.Paths[pathID]; existed {
				t.pathChangeChan <- PathChange{
					Prefix:       prefix,
					IsWithdrawal: false,
					IsSamePath:   asPath.Equals(oldPath),
					OldPath:      oldPath,
				}
				t.pathChangeCount.Add(1)
			} else {
				if t.importCount.Add(1) > config.GlobalConf.ImportLimit {
					t.importCount.Add(^uint32(0))
					t.sessionCancellation(notification.ErrImportLimit)
					return
				}
			}
		}
		entry.Paths[pathID] = asPath
	}
}

func (t *PrefixTable) ImportCount() uint32 {
	return t.importCount.Load()
}

// SwapPathChangeCount atomically returns and resets the per-session path
// change counter. Intended to be called once per statistics collection
// interval by the monitor so that the returned delta reflects the number
// of BGP update events produced by this session during that window. The
// returned value is also stored in lastIntervalCount so that
// non-destructive readers can observe the most recent interval's rate.
func (t *PrefixTable) SwapPathChangeCount() uint64 {
	v := t.pathChangeCount.Swap(0)
	t.lastIntervalCount.Store(v)
	return v
}

// LastIntervalCount returns the path-change count observed during the most
// recent statistics collection interval, without consuming the live
// counter. This is safe to call from HTTP request handlers.
func (t *PrefixTable) LastIntervalCount() uint64 {
	return t.lastIntervalCount.Load()
}
