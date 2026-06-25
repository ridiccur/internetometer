# internetometer

A console implementation of the **Yandex Internetometer**
(`yandex.com/internet`). It talks **exclusively to Yandex infrastructure** —
no third-party IP/geo/speed providers. A portable Go core (`./core`,
standard library only) powers the CLI today and binds into iOS/Android
tomorrow via gomobile.

```
core/     Yandex-only measurement library (mobile-bindable, no deps)
mobile/   gomobile surface — JSON-string API for Swift/Kotlin
cmd/      the console application
```

## CLI

```bash
go build -o bin/internetometer ./cmd/internetometer

./bin/internetometer              # IP, ASN, region, VPN flag
./bin/internetometer -speed       # also run latency/download/upload (live progress bar)
./bin/internetometer -lang ru     # use yandex.ru instead of yandex.com
./bin/internetometer -json        # machine-readable output
```

## What it measures — and the Yandex source for each

| Field                | Yandex source                                              |
|----------------------|------------------------------------------------------------|
| IPv4                 | `ipv4-internet.yandex.net/api/v0/ip`                       |
| IPv6                 | `ipv6-internet.yandex.net/api/v0/ip`                       |
| ASN, VPN flag        | `isp` object embedded in the Internetometer page state     |
| Region (by IP / cfg) | `clientRegion` object in the page state                    |
| Latency / Download / Upload | CDN probes from `yandex.{com,ru}/internet/api/v0/get-probes`, run against `*.cdn.yandex.net` |
| Measurement ID       | `mid` from get-probes                                      |
| OS / Arch / CPUs / TZ | local runtime                                             |

Notes:
- Yandex exposes the provider only as an **ASN number** (`AS28840`); it does
  not return the provider's display name, so neither do we.
- Latency uses TCP/HTTP round-trip timing to the CDN ping probes rather than
  ICMP, which is unavailable in sandboxed iOS/Android apps.
- The speed test streams for ~8s per direction, matching Yandex's window.

## Mobile integration

The `mobile` package exposes only JSON-returning functions, staying within
gomobile's supported type set.

```bash
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init

gomobile bind -target=ios     -o Internetometer.xcframework ./mobile   # iOS
gomobile bind -target=android -o internetometer.aar          ./mobile   # Android
```

Host side — call `GatherInfoJSON()` (fast: IP/ASN/region) or `GatherJSON()`
(full, with speed test) and decode into native models matching the `Report`
struct in `core/report.go`:

```swift
let json = MobileGatherInfoJSON()
let report = try JSONDecoder().decode(Report.self, from: Data(json.utf8))
```
```kotlin
val report = Json.decodeFromString<Report>(Mobile.gatherInfoJSON())
```

## Credits

The Yandex endpoint logic (get-probes flow, probe selection, page-state
fields) is adapted from
[Master290/internetometer-cli](https://github.com/Master290/internetometer-cli)
(MIT). This project restructures it into a dependency-free, mobile-bindable
core and drops that project's third-party ISP lookup (`ip-api.com`) in favor
of Yandex's own `isp` page field.
