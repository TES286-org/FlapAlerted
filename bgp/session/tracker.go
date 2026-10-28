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
	type JSONInfo struct {
		Remote        string
		RouterID      string
		Hostname      string
		EstablishTime int64
		ImportCount   uint32
	}

	var sessions = make([]JSONInfo, 0)
	for _, session := range sessionTracker {
		sessions = append(sessions, JSONInfo{
			Remote:        session.Remote,
			RouterID:      session.session.RemoteRouterID.String(),
			Hostname:      session.session.RemoteHostname,
			EstablishTime: session.EstablishTime,
			ImportCount:   session.table.ImportCount(),
		})
	}
	result, err := json.Marshal(sessions)
	if err != nil {
		return "", err
	}
	return string(result), nil
}
