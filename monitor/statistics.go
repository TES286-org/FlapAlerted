package monitor

import (
	"FlapAlerted/analyze"
	"FlapAlerted/bgp/session"
	"cmp"
	"context"
	"math"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const NumDataPoints = 51

func GetAverageRouteChanges90() float64 {
	stats := GetStats()
	changesList := make([]uint64, len(stats))
	for i, stat := range stats {
		changesList[i] = stat.Stats.Changes
	}
	slices.Sort(changesList)
	cutLength := int(math.Ceil(float64(len(changesList)) * 0.90))
	changesList = changesList[:cutLength]

	if len(changesList) == 0 {
		return 0
	}

	var sum uint64 = 0
	for _, u := range changesList {
		sum += u
	}
	avg := float64(sum) / float64(len(changesList))
	return avg / statisticsCollectionIntervalSec
}

func GetSessionInfoJson() (string, error) {
	return session.GetSessionInfoJson()
}

type Metric struct {
	ActiveFlapCount                int
	ActiveFlapTotalPathChangeCount uint64
	TotalPathChangeCount           uint64
	AverageRouteChanges90          string
	Sessions                       int
}

func GetMetric() Metric {
	var activeFlapCount int
	var activeFlapTotalPathChangeCount uint64
	var pathChangeCount uint64
	stats := GetStats()
	if len(stats) != 0 {
		activeFlapCount = stats[len(stats)-1].Stats.Active
		activeFlapTotalPathChangeCount = stats[len(stats)-1].Stats.ListedChanges
		pathChangeCount = stats[len(stats)-1].Stats.Changes
	}
	avg := GetAverageRouteChanges90()
	avgStr := strconv.FormatFloat(avg, 'f', 2, 64)

	return Metric{
		ActiveFlapCount:                activeFlapCount,
		ActiveFlapTotalPathChangeCount: activeFlapTotalPathChangeCount,
		TotalPathChangeCount:           pathChangeCount,
		AverageRouteChanges90:          avgStr,
		Sessions:                       session.GetSessionCount(),
	}
}

// -------------------------------------------------------
const statisticsCollectionIntervalSec = 5

type statisticWrapper struct {
	List         []FlapSummary
	ListPeers    []PeerSummary
	ListSessions []SessionSummary
	Stats        statistic
	Sessions     int
}
type FlapSummary struct {
	Prefix     string
	FirstSeen  int64
	RateSec    int
	TotalCount uint64
}

type PeerSummary struct {
	ASN        uint32
	RateSecAvg float64
	RateSec    int
}

type statistic struct {
	Time              int64
	Changes           uint64
	SamePathChanges   uint64
	WithdrawalChanges uint64
	ListedChanges     uint64
	Active            int
	RouteCount        uint64
}

// SessionSummary represents one BGP session's update activity for the most
// recent collection interval. It is consumed by the per-session rate chart.
// RateSec is the raw count observed during the (5s) collection window; the
// frontend divides by the interval to obtain a per-second rate.
type SessionSummary struct {
	Label   string `json:"Label"`
	RateSec int    `json:"RateSec"`
}

var (
	statList     = make([]statistic, 0)
	statListLock sync.RWMutex

	statSubscribers     = make([]chan statisticWrapper, 0)
	statSubscribersLock sync.Mutex
)

func SubscribeToStats() chan statisticWrapper {
	statSubscribersLock.Lock()
	defer statSubscribersLock.Unlock()
	c := make(chan statisticWrapper, 2)
	statSubscribers = append(statSubscribers, c)
	return c
}

var lastFlapSummaryList atomic.Pointer[[]FlapSummary]
var lastPeerSummaryList atomic.Pointer[[]PeerSummary]
var lastSessionSummaryList atomic.Pointer[[]SessionSummary]

func statTracker(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(statisticsCollectionIntervalSec) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}

		aFlap, trackedCount := analyze.GetActiveFlapList()
		aPeer := analyze.GetPeerRates()
		sessionRates := session.GetSessionRates()

		if len(aFlap) > 100 {
			slices.SortFunc(aFlap, func(a, b analyze.FlapEvent) int {
				return cmp.Compare(b.TotalPathChanges, a.TotalPathChanges)
			})
			aFlap = aFlap[:100]
		}

		if len(aPeer) > 30 {
			slices.SortFunc(aPeer, func(a, b analyze.PeerUpdateRate) int {
				return cmp.Compare(b.RateSecAvg, a.RateSecAvg)
			})
			aPeer = aPeer[:30]
		}

		// Keep the per-session chart readable when there are many feeds:
		// surface the top sessions by current interval rate and aggregate any
		// remaining sessions into a single "others" bucket so the chart still
		// represents the total activity without an explosion of datasets.
		const maxSessionDatasets = 20
		jsSessionList := buildSessionSummaryList(sessionRates, maxSessionDatasets)

		jsFlapList := make([]FlapSummary, len(aFlap))
		for i, f := range aFlap {
			jsFlapList[i] = FlapSummary{
				Prefix:     f.Prefix.String(),
				FirstSeen:  f.FirstSeen,
				RateSec:    f.RateSec,
				TotalCount: f.TotalPathChanges,
			}
		}

		jsPeerList := make([]PeerSummary, len(aPeer))
		for i, p := range aPeer {
			jsPeerList[i] = PeerSummary{
				ASN:        p.PeerASN,
				RateSec:    p.RateSec,
				RateSecAvg: p.RateSecAvg,
			}
		}

		lastFlapSummaryList.Store(&jsFlapList)
		lastPeerSummaryList.Store(&jsPeerList)
		lastSessionSummaryList.Store(&jsSessionList)

		newStatistic := statistic{
			Time:              time.Now().Unix(),
			Changes:           analyze.GlobalTotalRouteChangeCounter.Swap(0),
			SamePathChanges:   analyze.GlobalSamePathChangeCounter.Swap(0),
			WithdrawalChanges: analyze.GlobalWithdrawalCounter.Swap(0),
			ListedChanges:     analyze.GlobalListedRouteChangeCounter.Swap(0),
			Active:            trackedCount,
			RouteCount:        session.GetTotalImportCount(),
		}

		newWrapper := statisticWrapper{
			List:         jsFlapList,
			ListPeers:    jsPeerList,
			ListSessions: jsSessionList,
			Stats:        newStatistic,
			Sessions:     session.GetSessionCount(),
		}

		statSubscribersLock.Lock()
		for _, subscriber := range statSubscribers {
			select {
			case subscriber <- newWrapper:
			default:
			}
		}
		statSubscribersLock.Unlock()

		statListLock.Lock()
		statList = append(statList, newStatistic)
		if len(statList) > NumDataPoints {
			statList = statList[1:]
		}
		statListLock.Unlock()
	}
}

// buildSessionSummaryList prepares the per-session rate list sent to the
// frontend. When the number of sessions exceeds maxDatasets, the lowest-
// rate sessions are aggregated into a single "others" bucket so the chart
// remains readable while still representing the total activity. Sessions
// with the same display label (e.g. duplicate hostnames) are also merged to
// avoid presenting two visually indistinguishable stacked datasets.
func buildSessionSummaryList(rates []session.SessionRate, maxDatasets int) []SessionSummary {
	if len(rates) == 0 {
		return make([]SessionSummary, 0)
	}

	// Aggregate by label so sessions sharing a hostname collapse into a
	// single chart dataset. This matches what the viewer actually sees.
	totals := make(map[string]int, len(rates))
	for _, r := range rates {
		totals[r.Label] += r.RateSec
	}

	list := make([]SessionSummary, 0, len(totals))
	for label, rate := range totals {
		list = append(list, SessionSummary{Label: label, RateSec: rate})
	}

	slices.SortFunc(list, func(a, b SessionSummary) int {
		return cmp.Compare(b.RateSec, a.RateSec)
	})

	if len(list) <= maxDatasets {
		return list
	}

	kept := list[:maxDatasets]
	othersRate := 0
	for _, s := range list[maxDatasets:] {
		othersRate += s.RateSec
	}
	kept = append(kept, SessionSummary{Label: "others", RateSec: othersRate})
	return kept
}

func GetStats() []statisticWrapper {
	statListLock.RLock()
	defer statListLock.RUnlock()
	result := make([]statisticWrapper, len(statList))
	for i := range statList {
		result[i] = statisticWrapper{
			List:         nil,
			ListPeers:    nil,
			ListSessions: nil,
			Stats:        statList[i],
			Sessions:     -1,
		}
	}
	if len(statList) > 0 {
		l := lastFlapSummaryList.Load()
		p := lastPeerSummaryList.Load()
		s := lastSessionSummaryList.Load()
		if l != nil {
			result[len(statList)-1].List = *l
			result[len(statList)-1].ListPeers = *p
			result[len(statList)-1].ListSessions = *s
			result[len(statList)-1].Sessions = session.GetSessionCount()
		}
	}
	return result
}

func GetActiveFlapsSummary() []FlapSummary {
	l := lastFlapSummaryList.Load()
	if l == nil {
		return make([]FlapSummary, 0)
	}
	return *l
}

func GetActivePeersSummary() []PeerSummary {
	p := lastPeerSummaryList.Load()
	if p == nil {
		return make([]PeerSummary, 0)
	}
	return *p
}

func GetActiveSessionsSummary() []SessionSummary {
	s := lastSessionSummaryList.Load()
	if s == nil {
		return make([]SessionSummary, 0)
	}
	return *s
}
