package session

import (
	"FlapAlerted/bgp/common"
	"FlapAlerted/bgp/table"
	"net"
	"net/netip"
	"testing"
	"time"
)

// remoteAddr wraps a string so net.SplitHostPort can parse it.
type remoteAddr string

func (a remoteAddr) Network() string { return "tcp" }
func (a remoteAddr) String() string  { return string(a) }

// hostPortConn is a minimal net.Conn whose RemoteAddr returns a fixed
// host:port string. Used to register sessions in tests without spinning
// up real TCP connections.
type hostPortConn struct {
	addr remoteAddr
}

func (c *hostPortConn) Read([]byte) (int, error)         { return 0, nil }
func (c *hostPortConn) Write([]byte) (int, error)        { return 0, nil }
func (c *hostPortConn) Close() error                     { return nil }
func (c *hostPortConn) LocalAddr() net.Addr              { return nil }
func (c *hostPortConn) RemoteAddr() net.Addr             { return c.addr }
func (c *hostPortConn) SetDeadline(time.Time) error      { return nil }
func (c *hostPortConn) SetReadDeadline(time.Time) error  { return nil }
func (c *hostPortConn) SetWriteDeadline(time.Time) error { return nil }

// TestSessionLabel_PrefersHostname verifies that the display label uses the
// BGP hostname capability value when the peer advertised one.
func TestSessionLabel_PrefersHostname(t *testing.T) {
	t.Cleanup(resetTrackerForTest)

	pathChangeChan := make(chan table.PathChange, 10)
	tbl := table.NewPrefixTable(pathChangeChan, nil)
	sess := &common.LocalSession{
		RemoteRouterID: netip.MustParseAddr("192.0.2.1"),
		RemoteHostname: "router-alpha",
	}
	conn := &hostPortConn{addr: remoteAddr("192.0.2.1:12345")}

	AddSession(conn, sess, tbl)
	defer RemoveSession(conn)

	infos := GetSessionInfos()
	if len(infos) != 1 {
		t.Fatalf("expected 1 session, got %d", len(infos))
	}
	if infos[0].Label != "router-alpha" {
		t.Errorf("expected label 'router-alpha', got %q", infos[0].Label)
	}
}

// TestSessionLabel_FallsBackToIPv4 verifies that when the peer did not
// advertise a hostname, the label falls back to the peer IPv4 address with
// the port stripped.
func TestSessionLabel_FallsBackToIPv4(t *testing.T) {
	t.Cleanup(resetTrackerForTest)

	pathChangeChan := make(chan table.PathChange, 10)
	tbl := table.NewPrefixTable(pathChangeChan, nil)
	sess := &common.LocalSession{
		RemoteRouterID: netip.MustParseAddr("192.0.2.1"),
		RemoteHostname: "",
	}
	conn := &hostPortConn{addr: remoteAddr("192.0.2.1:51234")}

	AddSession(conn, sess, tbl)
	defer RemoveSession(conn)

	infos := GetSessionInfos()
	if len(infos) != 1 {
		t.Fatalf("expected 1 session, got %d", len(infos))
	}
	if infos[0].Label != "192.0.2.1" {
		t.Errorf("expected label '192.0.2.1', got %q", infos[0].Label)
	}
}

// TestSessionLabel_FallsBackToIPv6 verifies the same fallback for IPv6
// addresses (where net.SplitHostPort must handle the bracket notation).
func TestSessionLabel_FallsBackToIPv6(t *testing.T) {
	t.Cleanup(resetTrackerForTest)

	pathChangeChan := make(chan table.PathChange, 10)
	tbl := table.NewPrefixTable(pathChangeChan, nil)
	sess := &common.LocalSession{
		RemoteRouterID: netip.MustParseAddr("fdcf:8538:9ad5::1"),
		RemoteHostname: "",
	}
	conn := &hostPortConn{addr: remoteAddr("[fdcf:8538:9ad5::1]:51234")}

	AddSession(conn, sess, tbl)
	defer RemoveSession(conn)

	infos := GetSessionInfos()
	if len(infos) != 1 {
		t.Fatalf("expected 1 session, got %d", len(infos))
	}
	if infos[0].Label != "fdcf:8538:9ad5::1" {
		t.Errorf("expected label 'fdcf:8538:9ad5::1', got %q", infos[0].Label)
	}
}

// TestGetSessionInfos_IncludesRate verifies that GetSessionInfos exposes a
// RateSec field. The non-destructive read semantics are covered by the
// table-package tests; here we only assert the field is present and defaults
// to zero before any stats collection interval has run.
func TestGetSessionInfos_IncludesRate(t *testing.T) {
	t.Cleanup(resetTrackerForTest)

	pathChangeChan := make(chan table.PathChange, 10)
	tbl := table.NewPrefixTable(pathChangeChan, nil)
	sess := &common.LocalSession{
		RemoteRouterID: netip.MustParseAddr("192.0.2.1"),
		RemoteHostname: "router-gamma",
	}
	conn := &hostPortConn{addr: remoteAddr("192.0.2.1:12345")}

	AddSession(conn, sess, tbl)
	defer RemoveSession(conn)

	infos := GetSessionInfos()
	if len(infos) != 1 {
		t.Fatalf("expected 1 session, got %d", len(infos))
	}
	// Before the stat tracker has swapped, RateSec must be 0 (not missing).
	if infos[0].RateSec != 0 {
		t.Errorf("expected RateSec=0 before first collection, got %d", infos[0].RateSec)
	}
}

// resetTrackerForTest clears the global session tracker so tests don't
// interfere with each other.
func resetTrackerForTest() {
	sessionTrackerLock.Lock()
	defer sessionTrackerLock.Unlock()
	sessionTracker = make(map[net.Conn]establishedSession)
}
