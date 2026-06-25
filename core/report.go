// Package core is a portable implementation of the Yandex Internetometer
// (yandex.com/internet). It talks exclusively to Yandex infrastructure —
// the IP echo hosts (ipv4/ipv6-internet.yandex.net), the Internetometer
// page state, and the CDN probes from get-probes — with no third-party
// IP/geo/speed providers.
//
// It uses only the Go standard library, so the same package compiles into
// the CLI in ../cmd/internetometer and binds into an iOS .xcframework /
// Android .aar via gomobile (see ../mobile).
package core

import (
	"context"
	"encoding/json"
	"runtime"
	"sync"
	"time"
)

// Report is the full snapshot of the connection, as Yandex sees it.
type Report struct {
	IPv4      string       `json:"ipv4,omitempty"`
	IPv6      string       `json:"ipv6,omitempty"`
	ASN       int          `json:"asn,omitempty"`
	IsVPN     bool         `json:"is_vpn"`
	Region    *RegionInfo  `json:"region,omitempty"`
	Speed     *SpeedResult `json:"speed,omitempty"`
	System    SystemInfo   `json:"system"`
	MID       string       `json:"measurement_id,omitempty"`
	Timestamp string       `json:"timestamp"`

	// Errors collects non-fatal failures so partial results stay usable.
	Errors []string `json:"errors,omitempty"`
}

// RegionInfo is location as reported by Yandex. Name is the region
// configured in Yandex settings; ByIP is the region detected from the IP.
type RegionInfo struct {
	Name string `json:"name,omitempty"`
	ByIP string `json:"by_ip,omitempty"`
	ID   int    `json:"id,omitempty"`
}

// SpeedResult holds throughput (Mbps) and latency measured against the
// Yandex CDN probes.
type SpeedResult struct {
	DownloadMbps float64 `json:"download_mbps"`
	UploadMbps   float64 `json:"upload_mbps"`
	LatencyMs    float64 `json:"latency_ms,omitempty"`
}

// SystemInfo is locally available device/runtime info.
type SystemInfo struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	NumCPU   int    `json:"num_cpu"`
	Timezone string `json:"timezone"`
	GoVer    string `json:"go_version"`
}

// Speed-test phases reported through a ProgressFunc.
const (
	PhaseLatency  = "latency"
	PhaseDownload = "download"
	PhaseUpload   = "upload"
)

// Progress is a single update emitted during the speed test. Fraction is
// the 0..1 position within the current phase's time window.
type Progress struct {
	Phase    string  `json:"phase"`
	Mbps     float64 `json:"mbps"`
	Fraction float64 `json:"fraction"`
}

// ProgressFunc receives speed-test progress updates. It may be called from
// a goroutine and is throttled by the core (~12/s), so it can drive a UI
// directly. Kept out of the gomobile surface (function-typed fields don't
// bind); the CLI uses it.
type ProgressFunc func(Progress)

// Options controls what an invocation measures.
type Options struct {
	// RunSpeedTest enables the (slower, bandwidth-using) latency + download
	// + upload test against the Yandex CDN.
	RunSpeedTest bool
	// Language selects the page: "en" -> yandex.com, "ru" -> yandex.ru.
	Language string
	// Timeout bounds the whole gather. Zero means a sane default.
	Timeout time.Duration
	// OnProgress, if set, receives live speed-test updates.
	OnProgress ProgressFunc
}

// Gather collects everything according to opts. IP detection and the page
// state are fetched concurrently; the speed test (if enabled) runs after,
// as it saturates the link and would skew latency.
func Gather(ctx context.Context, opts Options) *Report {
	if opts.Timeout <= 0 {
		if opts.RunSpeedTest {
			opts.Timeout = 90 * time.Second
		} else {
			opts.Timeout = 30 * time.Second
		}
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	c := newYandexClient(opts.Language)
	r := &Report{
		System:    localSystemInfo(),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	var mu sync.Mutex
	addErr := func(e error) {
		if e == nil {
			return
		}
		mu.Lock()
		r.Errors = append(r.Errors, e.Error())
		mu.Unlock()
	}

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		ip, err := c.publicIPv4(ctx)
		addErr(err)
		r.IPv4 = ip
	}()
	go func() {
		defer wg.Done()
		// A missing IPv6 is normal, not an error worth surfacing.
		if ip, err := c.publicIPv6(ctx); err == nil {
			r.IPv6 = ip
		}
	}()
	go func() {
		defer wg.Done()
		st, err := c.fetchPageState(ctx)
		addErr(err)
		if st != nil {
			r.ASN = st.ASN
			r.IsVPN = st.IsVPN
			r.Region = &st.Region
			// Fall back to the page's IPs if an echo host failed.
			if r.IPv4 == "" {
				r.IPv4 = st.IPv4
			}
			if r.IPv6 == "" {
				r.IPv6 = st.IPv6
			}
		}
	}()
	wg.Wait()

	if opts.RunSpeedTest {
		if probes, err := c.getProbes(ctx); err != nil {
			addErr(err)
		} else {
			sp, mid, err := c.runSpeedTest(ctx, probes, opts.OnProgress)
			addErr(err)
			r.Speed = sp
			r.MID = mid
		}
	}
	return r
}

// JSON renders the report as indented JSON.
func (r *Report) JSON() string {
	b, _ := json.MarshalIndent(r, "", "  ")
	return string(b)
}

func localSystemInfo() SystemInfo {
	zone, _ := time.Now().Zone()
	return SystemInfo{
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		NumCPU:   runtime.NumCPU(),
		Timezone: zone,
		GoVer:    runtime.Version(),
	}
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}
