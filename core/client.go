package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Yandex Internetometer infrastructure. We talk ONLY to these hosts — no
// third-party IP/geo/speed providers are used.
const (
	baseURLCom = "https://yandex.com/internet"
	baseURLRu  = "https://yandex.ru/internet"

	ipv4EchoURL = "https://ipv4-internet.yandex.net/api/v0/ip"
	ipv6EchoURL = "https://ipv6-internet.yandex.net/api/v0/ip"

	// browserUA mimics a real browser; the API rejects unknown agents.
	browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// client wraps the HTTP clients used to reach Yandex. We keep three:
// a default one (resolver picks the family) and two pinned to IPv4/IPv6 so
// the ipv4-/ipv6- echo hosts are reached over the right protocol.
type client struct {
	lang     string // "en" -> yandex.com, "ru" -> yandex.ru
	def      *http.Client
	v4, v6   *http.Client
}

func newYandexClient(lang string) *client {
	return &client{
		lang: lang,
		def:  httpClient("tcp", 30*time.Second),
		v4:   httpClient("tcp4", 15*time.Second),
		v6:   httpClient("tcp6", 15*time.Second),
	}
}

// baseURL returns the language-appropriate Internetometer page URL.
func (c *client) baseURL() string {
	if c.lang == "ru" {
		return baseURLRu
	}
	return baseURLCom
}

// httpClient builds a client whose dialer is pinned to the given network
// ("tcp", "tcp4" or "tcp6"). timeout==0 means no blanket timeout (used for
// the speed test, which is bounded by context instead).
func httpClient(network string, timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        16,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}

// getJSON GETs url with browser headers and decodes the JSON body into v.
func (c *client) getJSON(ctx context.Context, hc *http.Client, url string, v any) error {
	body, err := c.getBytes(ctx, hc, url, "application/json")
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

// getBytes GETs url and returns the (size-limited) body.
func (c *client) getBytes(ctx context.Context, hc *http.Client, url, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set("Referer", c.baseURL())

	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}
