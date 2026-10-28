package session

import (
	"FlapAlerted/bgp/common"
	"FlapAlerted/bgp/table"
	"encoding/json"
	"net"
	"sync"
	"time"
)

// Established session tracker
var (
	sessionTracker     = make(map[net.Conn]establishedSession)
	sessionTrackerLock sync.RWMutex
)

type establishedSession struct {
	Remote        string
	EstablishTime int64
	session       *common.LocalSession
	table         *table.PrefixTable
}

// SessionRate describes the per-session BGP update activity observed
// during a single statistics collection interval. Label is intended for
// direct display: the peer hostname when advertised, otherwise the peer
// IP address (port stripped).
type SessionRate struct {
	Label   string `json:"Label"`
	RateSec int    `json:"RateSec"`
}

// SessionInfo captures the display-relevant fields of an established BGP
// session. It is the structured equivalent of the JSONInfo type previously
// inlined in GetSessionInfoJson, factored out so that other callers (e.g.
// the Prometheus exporter, the /sessions endpoint) can consume the same
// data without re-marshalling.
type SessionInfo struct {
	Remote   string
	RouterID string
	Hostname string
	// Label is the display label resolved via the same rules as the
	// per-session rate chart: hostname when advertised, otherwise the
	// peer IP (port stripped). Exposed so external consumers (Prometheus,
	// sessions dialog) don't have to re-implement the fallback logic.
	Label         string
	EstablishTime int64
	ImportCount   uint32
	// RateSec is the path-change count observed during the most recent
	// statistics collection interval (non-destructive read). Dividing by
	// the collection interval (5s) yields the per-second rate.
	RateSec uint64
}

func AddSession(conn net.Conn, session *common.LocalSession, table *table.PrefixTable) {
	newSession := establishedSession{
		Remote:        conn.RemoteAddr().String(),
		EstablishTime: time.Now().Unix(),
		session:       session,
		table:         table,
	}
	sessionTrackerLock.Lock()
	defer sessionTrackerLock.Unlock()
	sessionTracker[conn] = newSession
}

func RemoveSession(conn net.Conn) {
	sessionTrackerLock.Lock()
	defer sessionTrackerLock.Unlock()
	delete(sessionTracker, conn)
}

func GetSessionCount() int {
	sessionTrackerLock.RLock()
	defer sessionTrackerLock.RUnlock()
	return len(sessionTracker)
}

func GetTotalImportCount() uint64 {
	sessionTrackerLock.RLock()
	defer sessionTrackerLock.RUnlock()
	var totalCount uint64
	for _, session := range sessionTracker {
		totalCount += uint64(session.table.ImportCount())
	}
	return totalCount
}

// GetSessionRates swaps each session's path change counter and returns a
// snapshot suitable for the per-session rate chart. The caller (statistics
// collector) is expected to invoke this once per collection interval so
// that the returned deltas reflect activity during that window.
//
// Label assignment follows the user-facing requirement: the peer
// hostname is used when advertised (non-empty), otherwise the peer IP
// address is used (the port component of the remote address is stripped).
func GetSessionRates() []SessionRate {
	sessionTrackerLock.RLock()
	defer sessionTrackerLock.RUnlock()

	rates := make([]SessionRate, 0, len(sessionTracker))
	for _, s := range sessionTracker {
		count := s.table.SwapPathChangeCount()
		rates = append(rates, SessionRate{
			Label:   sessionLabel(s),
			RateSec: int(count),
		})
	}
	return rates
}

// sessionLabel resolves the display label for an established session,
// preferring the BGP hostname capability value and falling back to the
// peer IP address (port stripped) when the peer did not advertise one.
func sessionLabel(s establishedSession) string {
	if s.session != nil && s.session.RemoteHostname != "" {
		return s.session.RemoteHostname
	}
	if host, _, err := net.SplitHostPort(s.Remote); err == nil && host != "" {
		return host
	}
	return s.Remote
}

func GetSessionInfoJson() (string, error) {
	sessionTrackerLock.RLock()
	defer sessionTrackerLock.RUnlock()

	infos := GetSessionInfosLocked()
	result, err := json.Marshal(infos)
	if err != nil {
		return "", err
	}
	return string(result), nil
}

// GetSessionInfos returns a snapshot of all established sessions including
// the most recent interval's path-change rate. Safe to call from HTTP
// request handlers; does not consume the live counter.
func GetSessionInfos() []SessionInfo {
	sessionTrackerLock.RLock()
	defer sessionTrackerLock.RUnlock()
	return GetSessionInfosLocked()
}

// GetSessionInfosLocked is the lock-free inner helper. Callers must hold
// sessionTrackerLock (read or write). Factored out so that GetSessionInfoJson
// and GetSessionInfos share a single implementation.
func GetSessionInfosLocked() []SessionInfo {
	infos := make([]SessionInfo, 0, len(sessionTracker))
	for _, s := range sessionTracker {
		infos = append(infos, SessionInfo{
			Remote:        s.Remote,
			RouterID:      s.session.RemoteRouterID.String(),
			Hostname:      s.session.RemoteHostname,
			Label:         sessionLabel(s),
			EstablishTime: s.EstablishTime,
			ImportCount:   s.table.ImportCount(),
			RateSec:       s.table.LastIntervalCount(),
		})
	}
	return infos
}
