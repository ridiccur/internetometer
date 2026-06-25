package core

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
)

// The Internetometer page embeds its initial state as JSON in the HTML.
// We pull the three objects we need straight from that state — these are
// Yandex's own values, the same ones the page renders:
//
//	"ip":{"v4":"178.206.176.139","v6":null}
//	"isp":{"asn":[28840],"localName":null,"isVpn":false}
//	"clientRegion":{"name":"Semekeyevo","ip":"Naberezhnye Chelny","id":236}
//
// The objects are flat (no nested braces), so a bounded regex is robust.
var (
	reIP     = regexp.MustCompile(`"ip":(\{[^{}]*\})`)
	reISP    = regexp.MustCompile(`"isp":(\{[^{}]*\})`)
	reRegion = regexp.MustCompile(`"clientRegion":(\{[^{}]*\})`)
)

type pageState struct {
	IPv4   string
	IPv6   string
	ASN    int
	IsVPN  bool
	Region RegionInfo
}

type rawIP struct {
	V4 string `json:"v4"`
	V6 string `json:"v6"`
}

type rawISP struct {
	ASN       []int  `json:"asn"`
	LocalName string `json:"localName"`
	IsVPN     bool   `json:"isVpn"`
}

type rawRegion struct {
	Name string `json:"name"` // region configured in Yandex settings
	IP   string `json:"ip"`   // region detected from the IP address
	ID   int    `json:"id"`
}

// fetchPageState downloads the Internetometer HTML and extracts the
// embedded ip / isp / clientRegion objects.
func (c *client) fetchPageState(ctx context.Context) (*pageState, error) {
	body, err := c.getBytes(ctx, c.def, c.baseURL(), "text/html")
	if err != nil {
		return nil, err
	}

	st := &pageState{}

	if m := reIP.FindSubmatch(body); m != nil {
		var ip rawIP
		if json.Unmarshal(m[1], &ip) == nil {
			st.IPv4, st.IPv6 = ip.V4, ip.V6
		}
	}
	if m := reISP.FindSubmatch(body); m != nil {
		var isp rawISP
		if json.Unmarshal(m[1], &isp) == nil {
			if len(isp.ASN) > 0 {
				st.ASN = isp.ASN[0]
			}
			st.IsVPN = isp.IsVPN
		}
	}
	if m := reRegion.FindSubmatch(body); m != nil {
		var r rawRegion
		if json.Unmarshal(m[1], &r) == nil {
			st.Region = RegionInfo{Name: r.Name, ByIP: r.IP, ID: r.ID}
		}
	}

	if st.IPv4 == "" && st.Region.ByIP == "" && st.ASN == 0 {
		return nil, fmt.Errorf("could not parse Yandex page state")
	}
	return st, nil
}
