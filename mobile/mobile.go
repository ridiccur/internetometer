// Package mobile is the gomobile-facing surface. It exposes only
// JSON-string-returning functions so the binding stays within gomobile's
// supported type set (no structs, slices, contexts or durations cross the
// boundary). Build with:
//
//	gomobile bind -target=ios     -o Internetometer.xcframework ./mobile
//	gomobile bind -target=android -o internetometer.aar          ./mobile
//
// On the host side call GatherJSON / GatherInfoJSON and decode the JSON
// into your Swift/Kotlin models.
package mobile

import (
	"context"
	"time"

	"internetometer/core"
)

// GatherInfoJSON returns IP, geolocation and latency (no speed test) as
// JSON. Fast: suitable for a screen that loads on open.
func GatherInfoJSON() string {
	return gather(false, 20)
}

// GatherJSON returns the full report including the download/upload speed
// test as JSON. Slower and bandwidth-using; trigger it from a button.
func GatherJSON() string {
	return gather(true, 60)
}

func gather(speed bool, timeoutSec int) string {
	r := core.Gather(context.Background(), core.Options{
		RunSpeedTest: speed,
		Timeout:      time.Duration(timeoutSec) * time.Second,
	})
	return r.JSON()
}
