package core

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// get-probes hands back the CDN URLs Yandex uses to measure this client:
// ping endpoints, download files (100kb / 50mb) and an upload sink, all on
// *.cdn.yandex.net, picked for the caller's network.
const getProbesPath = "/api/v0/get-probes"

// probeWindow is how long we stream download/upload, matching Yandex's own
// ~8s measurement window.
const probeWindow = 8 * time.Second

type probe struct {
	URL     string `json:"url"`
	Size    int    `json:"size,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

type probesResponse struct {
	MID      string `json:"mid"`
	Latency  struct{ Probes []probe } `json:"latency"`
	Download struct{ Probes []probe } `json:"download"`
	Upload   struct{ Probes []probe } `json:"upload"`
}

func (c *client) getProbes(ctx context.Context) (*probesResponse, error) {
	var pr probesResponse
	url := c.baseURL() + getProbesPath
	if err := c.getJSON(ctx, c.def, url, &pr); err != nil {
		return nil, fmt.Errorf("get-probes: %w", err)
	}
	return &pr, nil
}

// runSpeedTest measures latency, download and upload against the probes.
// The speed client has no blanket timeout — the context bounds it.
func (c *client) runSpeedTest(ctx context.Context, probes *probesResponse, progress ProgressFunc) (*SpeedResult, string, error) {
	hc := httpClient("tcp", 0)
	res := &SpeedResult{}

	res.LatencyMs = c.measureLatency(ctx, hc, probes.Latency.Probes)

	if url := selectDownloadProbe(probes.Download.Probes); url != "" {
		mbps, err := c.measureDownload(ctx, hc, url, progress)
		if err != nil {
			return res, probes.MID, fmt.Errorf("download: %w", err)
		}
		res.DownloadMbps = mbps
	}
	if url := selectUploadProbe(probes.Upload.Probes); url != "" {
		mbps, err := c.measureUpload(ctx, hc, url, progress)
		if err != nil {
			return res, probes.MID, fmt.Errorf("upload: %w", err)
		}
		res.UploadMbps = mbps
	}
	return res, probes.MID, nil
}

// reporter emits throttled Progress updates for one phase, computing the
// throughput from the running byte total and elapsed time.
type reporter struct {
	progress ProgressFunc
	phase    string
	start    time.Time
	last     time.Time
}

func newReporter(progress ProgressFunc, phase string, start time.Time) *reporter {
	return &reporter{progress: progress, phase: phase, start: start}
}

func (r *reporter) emit(total int64, final bool) {
	if r.progress == nil {
		return
	}
	now := time.Now()
	if !final && now.Sub(r.last) < 80*time.Millisecond {
		return
	}
	elapsed := now.Sub(r.start)
	// Skip the first sub-150ms samples: throughput over a near-zero
	// window is meaningless and produces wild spikes.
	if !final && elapsed < 150*time.Millisecond {
		return
	}
	r.last = now
	frac := elapsed.Seconds() / probeWindow.Seconds()
	if frac > 1 {
		frac = 1
	}
	r.progress(Progress{Phase: r.phase, Mbps: mbps(total, elapsed), Fraction: frac})
}

// progressReader counts bytes as they flow and notifies onRead.
type progressReader struct {
	r      io.Reader
	onRead func(n int)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	if n > 0 && pr.onRead != nil {
		pr.onRead(n)
	}
	return n, err
}

// selectDownloadProbe prefers the 50mb file (stable throughput) and avoids
// the 100kb warm-up probe.
func selectDownloadProbe(probes []probe) string {
	var fallback string
	for _, p := range probes {
		if p.URL == "" {
			continue
		}
		if fallback == "" {
			fallback = p.URL
		}
		if strings.Contains(p.URL, "50mb") {
			return p.URL
		}
	}
	return fallback
}

func selectUploadProbe(probes []probe) string {
	for _, p := range probes {
		if p.URL != "" {
			return p.URL
		}
	}
	return ""
}

// measureLatency pings each probe a few times and returns the minimum
// round-trip in milliseconds (0 if none succeeded).
func (c *client) measureLatency(ctx context.Context, hc *http.Client, probes []probe) float64 {
	const rounds = 3
	best := time.Duration(0)
	for i := 0; i < rounds; i++ {
		for _, p := range probes {
			if p.URL == "" || ctx.Err() != nil {
				continue
			}
			start := time.Now()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
			if err != nil {
				continue
			}
			req.Header.Set("User-Agent", browserUA)
			req.Header.Set("Referer", c.baseURL())
			resp, err := hc.Do(req)
			if err != nil {
				continue
			}
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				if d := time.Since(start); best == 0 || d < best {
					best = d
				}
			}
		}
	}
	if best == 0 {
		return 0
	}
	return round2(float64(best.Microseconds()) / 1000.0)
}

// measureDownload repeatedly fetches the probe for probeWindow and reports
// the average throughput in Mbps.
func (c *client) measureDownload(ctx context.Context, hc *http.Client, url string, progress ProgressFunc) (float64, error) {
	start := time.Now()
	rep := newReporter(progress, PhaseDownload, start)
	var total int64
	for time.Since(start) < probeWindow {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("User-Agent", browserUA)
		req.Header.Set("Referer", c.baseURL())
		resp, err := hc.Do(req)
		if err != nil {
			if total > 0 {
				break
			}
			return 0, err
		}
		body := &progressReader{r: resp.Body, onRead: func(n int) {
			total += int64(n)
			rep.emit(total, false)
		}}
		n, _ := io.Copy(io.Discard, body)
		resp.Body.Close()
		if n == 0 {
			break
		}
	}
	rep.emit(total, true)
	return mbps(total, time.Since(start)), nil
}

// measureUpload streams zero bytes to the upload sink for probeWindow and
// reports the average throughput in Mbps.
func (c *client) measureUpload(ctx context.Context, hc *http.Client, url string, progress ProgressFunc) (float64, error) {
	const chunk = 8 << 20 // 8 MB per POST
	start := time.Now()
	rep := newReporter(progress, PhaseUpload, start)
	var total int64
	for time.Since(start) < probeWindow {
		body := &progressReader{r: io.LimitReader(zeroReader{}, chunk), onRead: func(n int) {
			total += int64(n)
			rep.emit(total, false)
		}}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
		if err != nil {
			return 0, err
		}
		req.Header.Set("User-Agent", browserUA)
		req.Header.Set("Referer", c.baseURL())
		req.Header.Set("Content-Type", "application/octet-stream")
		req.ContentLength = chunk
		resp, err := hc.Do(req)
		if err != nil {
			if total > 0 {
				break
			}
			return 0, err
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			if total > 0 {
				break
			}
			return 0, fmt.Errorf("status %d", resp.StatusCode)
		}
	}
	rep.emit(total, true)
	return mbps(total, time.Since(start)), nil
}

func mbps(bytes int64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return round2(float64(bytes) * 8 / 1e6 / d.Seconds())
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}
