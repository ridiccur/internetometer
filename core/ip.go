package core

import (
	"context"
	"fmt"
	"net"
	"net/http"
)

// publicIPv4 asks Yandex's IPv4-only echo host for our address. The host
// has A records only, and the request is pinned to tcp4, so the answer is
// always the IPv4 the connection egresses from.
func (c *client) publicIPv4(ctx context.Context) (string, error) {
	return c.echoIP(ctx, c.v4, ipv4EchoURL)
}

// publicIPv6 is the IPv6 counterpart. A missing IPv6 is normal (the caller
// treats the empty result as "not available" rather than an error).
func (c *client) publicIPv6(ctx context.Context) (string, error) {
	return c.echoIP(ctx, c.v6, ipv6EchoURL)
}

// echoIP decodes the endpoint's JSON-string body (e.g. "178.206.176.139").
func (c *client) echoIP(ctx context.Context, hc *http.Client, url string) (string, error) {
	var ip string
	if err := c.getJSON(ctx, hc, url, &ip); err != nil {
		return "", err
	}
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("%s returned non-IP %q", url, ip)
	}
	return ip, nil
}
