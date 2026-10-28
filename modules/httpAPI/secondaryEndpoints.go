//go:build !disable_mod_httpAPI

package httpAPI

import (
	"FlapAlerted/analyze"
	"FlapAlerted/bgp/session"
	"FlapAlerted/monitor"
	"fmt"
	"net/http"
	"strconv"
)

func (m *Module) getActiveFlaps(w http.ResponseWriter, _ *http.Request) {
	m.sendAsJSON(w, monitor.GetActiveFlapsSummary())
}

func (m *Module) getActivePeers(w http.ResponseWriter, _ *http.Request) {
	m.sendAsJSON(w, monitor.GetActivePeersSummary())
}

func (m *Module) getCapabilities(w http.ResponseWriter, _ *http.Request) {
	m.sendAsJSON(w, monitor.GetCapabilities())
}

func (m *Module) getMetrics(w http.ResponseWriter, _ *http.Request) {
	m.sendAsJSON(w, monitor.GetMetric())
}

func getAvgRouteChanges(w http.ResponseWriter, _ *http.Request) {
	avg := monitor.GetAverageRouteChanges90()
	avgStr := strconv.FormatFloat(avg, 'f', 2, 64)
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(avgStr))
}

func getPrometheus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")

	metric := monitor.GetMetric()
	_, _ = fmt.Fprintln(w, "# HELP active_flap_count Number of actively flapping prefixes")
	_, _ = fmt.Fprintln(w, "# TYPE active_flap_count gauge")
	_, _ = fmt.Fprintln(w, "active_flap_count", metric.ActiveFlapCount)

	_, _ = fmt.Fprintln(w, "# HELP active_flap_route_change_count Number of path changes caused by actively flapping prefixes")
	_, _ = fmt.Fprintln(w, "# TYPE active_flap_route_change_count gauge")
	_, _ = fmt.Fprintln(w, "active_flap_route_change_count", metric.ActiveFlapTotalPathChangeCount)

	_, _ = fmt.Fprintln(w, "# HELP route_change_count Number of path changes by all prefixes")
	_, _ = fmt.Fprintln(w, "# TYPE route_change_count gauge")
	_, _ = fmt.Fprintln(w, "route_change_count", metric.TotalPathChangeCount)

	_, _ = fmt.Fprintln(w, "# HELP average_route_changes_90 90th percentile average of route changes over the last 250 seconds, as overall route changes per second")
	_, _ = fmt.Fprintln(w, "# TYPE average_route_changes_90 gauge")
	_, _ = fmt.Fprintln(w, "average_route_changes_90", metric.AverageRouteChanges90)

	_, _ = fmt.Fprintln(w, "# HELP sessions Number of connected BGP feeds")
	_, _ = fmt.Fprintln(w, "# TYPE sessions gauge")
	_, _ = fmt.Fprintln(w, "sessions", metric.Sessions)
}

func prometheusActivePeerRates(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")

	rates := analyze.GetPeerRates()

	_, _ = fmt.Fprintln(w, "# HELP bgp_updates_per_second BGP update rate per second for each peer")
	_, _ = fmt.Fprintln(w, "# TYPE bgp_updates_per_second gauge")

	_, _ = fmt.Fprintln(w, "# HELP bgp_updates_avg_per_second BGP update rate per second for each peer (averaged)")
	_, _ = fmt.Fprintln(w, "# TYPE bgp_updates_avg_per_second gauge")

	for _, r := range rates {
		if _, err := fmt.Fprintf(
			w, "bgp_updates_per_second{asn=\"%d\"} %d\n", r.PeerASN, r.RateSec,
		); err != nil {
			return
		}

		if _, err := fmt.Fprintf(
			w, "bgp_updates_avg_per_second{asn=\"%d\"} %.6f\n", r.PeerASN, r.RateSecAvg,
		); err != nil {
			return
		}
	}

	// Per-session metrics: one series per established BGP connection,
	// labeled with the peer hostname (when advertised) or the peer IP.
	// The rate reflects the most recent statistics collection interval
	// and is read non-destructively so it does not interfere with the
	// live counter consumed by the SSE stream.
	sessionInfos := session.GetSessionInfos()

	_, _ = fmt.Fprintln(w, "# HELP bgp_session_updates_per_interval BGP update count during the last collection interval for each session")
	_, _ = fmt.Fprintln(w, "# TYPE bgp_session_updates_per_interval gauge")

	_, _ = fmt.Fprintln(w, "# HELP bgp_session_imported_routes Number of imported routes for each session")
	_, _ = fmt.Fprintln(w, "# TYPE bgp_session_imported_routes gauge")

	for _, s := range sessionInfos {
		label := prometheusQuoteLabel(s.Label)
		if _, err := fmt.Fprintf(
			w, "bgp_session_updates_per_interval{label=%s} %d\n", label, s.RateSec,
		); err != nil {
			return
		}
		if _, err := fmt.Fprintf(
			w, "bgp_session_imported_routes{label=%s} %d\n", label, s.ImportCount,
		); err != nil {
			return
		}
	}
}

// prometheusQuoteLabel escapes and quotes a label value per the Prometheus
// exposition format (backslash, double-quote, newline are escaped). Go's %q
// verb produces exactly this escaping, so we delegate to fmt.Sprintf.
func prometheusQuoteLabel(value string) string {
	return fmt.Sprintf("%q", value)
}
