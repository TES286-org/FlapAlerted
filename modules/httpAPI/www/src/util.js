export function toTimeElapsed(secondsIn) {
    const secondsMinute = 60;
    const secondsHour = secondsMinute * 60;
    const secondsDay = secondsHour * 24;
    const days = Math.floor(secondsIn / secondsDay);
    const hours = Math.floor((secondsIn % secondsDay) / secondsHour).toString().padStart(2, "0");
    const minutes = Math.floor((secondsIn % secondsHour) / secondsMinute).toString().padStart(2, "0");
    const seconds = Math.floor(secondsIn % secondsMinute).toString().padStart(2, "0");
    let result = "";
    if (days !== 0) {
        result += `${days}d `;
    }
    result += `${hours}:${minutes}:${seconds}`;
    return result;
}

export function getRelativeTime(dateInput) {
    const date = dateInput instanceof Date ? dateInput : new Date(dateInput);
    const diffSeconds = Math.floor((Date.now() - date.getTime()) / 1000);

    const ranges = [
        { label: "year", seconds: 31536000 },
        { label: "month", seconds: 2592000 },
        { label: "day", seconds: 86400 },
        { label: "hour", seconds: 3600 },
        { label: "minute", seconds: 60 },
        { label: "second", seconds: 1 },
    ];

    for (const r of ranges) {
        const value = Math.floor(diffSeconds / r.seconds);
        if (value >= 1) {
            return `${value} ${r.label}${value !== 1 ? "s" : ""} ago`;
        }
    }

    return "just now";
}

const million = 1000000;
const billion = million * 1000;
const trillion = billion * 1000;

export function truncateRouteChanges(routeChanges) {
    if (routeChanges < million) {
        return routeChanges;
    } else if (routeChanges >= million && routeChanges < billion) {
        return `${Number(routeChanges / million).toFixed(3)} million`;
    } else if (routeChanges >= billion && routeChanges < trillion) {
        return `${Number(routeChanges / billion).toFixed(3)} billion`;
    } else if (routeChanges >= trillion) {
        return `${Number(routeChanges / trillion).toFixed(2)} trillion`;
    }
    return routeChanges;
}

// The collection interval (seconds) over which per-session path-change
// counts are accumulated. Mirrors monitor.statisticsCollectionIntervalSec
// on the backend. Kept here so the frontend can convert raw interval
// counts into a per-second rate without an extra round-trip.
export const STATS_COLLECTION_INTERVAL_SEC = 5;

// formatSessionRate converts a raw per-interval path-change count into a
// compact "N/s" display string. Returns "--" for missing/zero so the
// sessions dialog and totals row stay readable when there is no activity.
export function formatSessionRate(ratePerInterval) {
    const n = Number(ratePerInterval);
    if (!Number.isFinite(n) || n === 0) {
        return "0/s";
    }
    const perSec = n / STATS_COLLECTION_INTERVAL_SEC;
    // One decimal place is enough: the underlying counter is an integer
    // count over 5s, so sub-decimal precision is meaningless beyond 0.1.
    return `${perSec.toFixed(1)}/s`;
}