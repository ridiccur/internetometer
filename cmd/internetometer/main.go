// Command internetometer is a console front-end over the core package:
// a yandex.com/internet connection report for the terminal, using only
// Yandex infrastructure.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"internetometer/core"
)

func main() {
	var (
		runSpeed = flag.Bool("speed", false, "run the latency/download/upload test against the Yandex CDN")
		asJSON   = flag.Bool("json", false, "print raw JSON instead of a formatted report")
		lang     = flag.String("lang", "en", `Yandex locale: "en" (yandex.com) or "ru" (yandex.ru)`)
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	opts := core.Options{RunSpeedTest: *runSpeed, Language: *lang}
	if !*asJSON {
		fmt.Fprintln(os.Stderr, "Measuring connection via Yandex…")
		if *runSpeed && isTerminal(os.Stderr) {
			opts.OnProgress = newProgressBar()
		}
	}
	report := core.Gather(ctx, opts)
	if opts.OnProgress != nil {
		fmt.Fprintln(os.Stderr) // finish the last progress-bar line
	}

	if *asJSON {
		fmt.Println(report.JSON())
		return
	}
	fmt.Print(render(report))
}

func render(r *core.Report) string {
	var b strings.Builder
	line := func(label, value string) {
		if value == "" {
			value = "—"
		}
		fmt.Fprintf(&b, "  %-13s %s\n", label+":", value)
	}

	b.WriteString("\nInternet\n")
	line("IPv4", r.IPv4)
	line("IPv6", r.IPv6)

	b.WriteString("\nProvider & location\n")
	if r.ASN > 0 {
		line("ASN", fmt.Sprintf("AS%d", r.ASN))
	} else {
		line("ASN", "")
	}
	line("VPN", boolWord(r.IsVPN))
	if g := r.Region; g != nil {
		line("Region (IP)", g.ByIP)
		line("Region (cfg)", g.Name)
	}

	if s := r.Speed; s != nil {
		b.WriteString("\nSpeed\n")
		if s.LatencyMs > 0 {
			line("Latency", fmt.Sprintf("%.2f ms", s.LatencyMs))
		}
		line("Download", fmt.Sprintf("%.2f Mbps", s.DownloadMbps))
		line("Upload", fmt.Sprintf("%.2f Mbps", s.UploadMbps))
	}

	b.WriteString("\nSystem\n")
	line("OS / Arch", r.System.OS+" / "+r.System.Arch)
	line("CPUs", fmt.Sprintf("%d", r.System.NumCPU))
	line("Local TZ", r.System.Timezone)

	if r.MID != "" {
		line("Measure ID", r.MID)
	}

	if len(r.Errors) > 0 {
		b.WriteString("\nNotes\n")
		for _, e := range r.Errors {
			line("•", e)
		}
	}
	b.WriteString("\n")
	return b.String()
}

func boolWord(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// isTerminal reports whether f is a character device (a real terminal),
// so we skip the in-place progress bar when output is piped or redirected.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// newProgressBar returns a core.ProgressFunc that draws an in-place bar on
// stderr, starting a fresh line whenever the speed-test phase changes.
func newProgressBar() core.ProgressFunc {
	const width = 28
	lastPhase := ""
	return func(p core.Progress) {
		if p.Phase != lastPhase {
			if lastPhase != "" {
				fmt.Fprintln(os.Stderr)
			}
			lastPhase = p.Phase
		}
		filled := int(p.Fraction*float64(width) + 0.5)
		if filled > width {
			filled = width
		}
		bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
		fmt.Fprintf(os.Stderr, "\r  %-9s [%s] %7.1f Mbps", p.Phase, bar, p.Mbps)
	}
}
