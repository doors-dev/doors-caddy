> Work in progress — not yet ready for production use.

# doors-caddy

Caddy v2 modules for [Doors](https://github.com/doors-dev/doors) apps:
zero-interruption rolling deployments with cookie-based load balancing,
and geo-IP redirects.

## Architecture

This package provides three Caddy modules:

- **`doors_handler`** (`http.handlers.doors_handler`) — HTTP middleware that
  decrypts pod-IP tokens and stores upstreams in the request context.
- **`doors_upstreams`** (`http.reverse_proxy.upstreams.doors_upstreams`) —
  Reverse proxy upstream source that reads upstreams from the request context.
- **`doors_geo`** (`http.handlers.doors_geo`) — Geo-IP middleware that looks
  up visitor country by IP and redirects (307) if configured.

`doors_handler` and `doors_upstreams` communicate through Caddy's request context
via `common.SetUpstreams` / `common.GetUpstreams`. They must appear in the right
order:

```
Request → doors_handler → sets upstreams in context → reverse_proxy + doors_upstreams → proxies to upstream
```

`doors_geo` runs independently — it needs no other Doors modules.

`doors_handler` carries all configuration (secret, cookie name, upstream blocks).
`doors_upstreams` is configuration‑free — it only reads what `doors_handler`
wrote.

## Features

- **Zero-interruption rollouts** — draining pods stay reachable for existing
  sessions until they naturally end.
- **Load balancing** — distribute new sessions across servers with
  cookie-based server affinity.
- **Pod-level routing** — Doors system requests (`/~/*`) reach the exact
  pod that owns the session, even mid-rollout.
- **Invalid token handling** — system requests with unrecognised tokens
  receive `410 Gone`. The Doors client triggers a full reload, landing on
  the fresh deployment.
- **Static Caddy config** — hosts resolve to fresh deployments
  automatically; no config changes needed during a rollout.
- **Geo-IP redirects** — visitor IP matched against auto-updating country
  IP databases; requests from configured countries redirect to the matching
  domain. Passes through if no match or database not ready.

## How it works

### Per-server model

Each `upstream` block represents one server. `host` is a DNS name
(Kubernetes service) that resolves to the current fresh deployment.
`pod_cidr` covers all pods on that server — 1 fresh pod plus 0 or more
draining pods from past rollouts.

```
┌─────────────────────────────┐
│  10.0.0.0/24                │
│  ┌─────────┐  ┌──────────┐  │
│  │ fresh   │  │ draining │  │
│  │ 10.0.0.2│  │ 10.0.0.1 │  │
│  └─────────┘  └──────────┘  │
│  host: svc.ns.svc.local     │
└─────────────────────────────┘
```

### Request routing

**System requests** (`/~/{token}/...`). The token is an encrypted pod IP.
`doors_handler` decrypts it, matches the IP against upstream CIDRs, and
stores an upstream pointing to the pod IP directly, which
`doors_upstreams` then reads. This guarantees system calls always reach
the instance that owns the session — whether the pod is fresh or
draining. If the token fails to decrypt or matches no CIDR,
`doors_handler` returns `410 Gone` and the Doors client performs a full
page reload.

**Normal requests, single upstream**. `doors_handler` always stores the
host-based upstream. `doors_upstreams` reads it and the reverse proxy
routes to the host, which resolves to the fresh deployment.

**Normal requests, multiple upstreams**. `doors_handler` reads the
`upstream` cookie (encrypted pod IP, set by Doors via
`ServerIDCookieName`). It matches the IP against upstream CIDRs to store
the matching server's host, keeping the session on the same server. No
cookie means a new session — all upstream hosts are stored and Caddy's
load-balancing policy selects one.

### Doors integration

The Doors app encrypts its pod IP and passes the token as the server ID.
Doors then handles the rest automatically — system paths, session cookie
name, and the sticky `upstream` cookie.

```go
import "github.com/doors-dev/doors-caddy"

func main() {
    cipher, _ := doorscaddy.NewTokenCipher(os.Getenv("SECRET"))
    token := cipher.Encode(netip.MustParseAddr(os.Getenv("POD_IP")))

    app := doors.NewApp(page,
        doors.WithID(token),
        doors.WithConf(doors.Conf{
            ServerIDCookieName: "upstream",
        }),
    )

    server := &http.Server{Addr: ":8080", Handler: app}

    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGTERM)
    go func() {
        <-sigCh
        app.Drain(func() {
            server.Shutdown(context.Background())
        })
    }()

    server.ListenAndServe()
}
```

`doors.WithID(token)` wires everything:
- Doors system paths become `/~/<token>/...` — caddy decrypts the token
  from the URL.
- The session cookie is named `<token>`.
- `ServerIDCookieName: "upstream"` sets an additional cookie with
  name `upstream` and value `<token>`. Caddy reads it for server
  affinity.

When `app.Drain` is called:
- Link navigation triggers a full browser reload instead of in-instance
  navigation. The browser loads the target URL fresh, which Caddy routes
  to the new deployment via host DNS.
- The `upstream` cookie is suppressed while draining, so no new sessions
  are pinned to the old server.
- Existing sessions continue working — system requests still reach the
  draining pod by IP.
- The callback fires when the instance count reaches zero.

## Geo-IP redirects

`doors_geo` downloads regularly-updated IP-address-to-country databases in the
background and issues 307 (Temporary Redirect) responses when the visitor's
country matches a configured domain.

### Database updates

Two goroutines run in the background — one for IPv4, one for IPv6. Each
periodically fetches a gzipped tar archive of `.zone` files (CIDR lists per
country) from [ipdeny.com](https://www.ipdeny.com). The CIDR → country
mapping is loaded into a lock-free routing table ([bart](https://github.com/gaissmai/bart))
and swapped in atomically on each successful download, so lookups never block.

- **Update interval**: 24 h by default, configurable with `update_interval`.
- **Conditional requests**: `ETag` and `If-Modified-Since` avoid re‑fetching
  unchanged data (HTTP 304).
- **Exponential backoff**: on failure, retries start at 30 s and cap at 1 h,
  with jitter (±10 %) added to every wait.
- **Download timeout**: HTTP client timeout, 30 s default (`download_timeout`).
- **Max body size**: response body is capped at 8 MiB.

### Request handling

On each request:

1. Resolve the client IP (respects Caddy's
   [`trusted_proxies`](https://caddyserver.com/docs/caddyfile/options#trusted-proxies)).
2. Look up the IP in the current database to get an
   [ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)
   country code.
3. Look up the country code in the redirect map configured in the Caddyfile.
   If found and the current host does not already match, issue a
   `307 Temporary Redirect` to `https://<target-domain>/<path>`.

If the visitor is already on the target domain (the current host matches
the redirect domain), no redirect is performed — the request passes through,
preventing redirect loops. Similarly, if the database is not yet loaded,
the country code is unknown, or there is no redirect configured for that
country, the request passes through to the next handler unchanged.

### Coverage

All country codes that should redirect must be **explicitly** listed.
There is no catch-all — visitors from uncovered countries continue to the
next handler. Country codes must not overlap across domains (behaviour is
undefined). For correct routing every country you care about must appear
in exactly one domain block.

## Rolling deployment

1. Pod `10.0.0.1` running on server `svc.ns.svc.local`. Caddy routes all
   traffic there.
2. New pod `10.0.0.2` deployed. `svc.ns.svc.local` DNS now resolves
   to the new pod.
3. Old pod receives SIGTERM, calls `app.Drain(...)`. Link clicks reload
   to the new pod. System requests still reach `10.0.0.1` by IP.
4. All sessions end. Drain callback fires. Pod terminates.
5. Server back to one fresh pod.

## Secret

```sh
openssl rand -base64 32
```

The same AES key must be set in both the Doors app (`SECRET` env) and the
Caddy config (`secret` directive).

## Build

Build a custom Caddy binary with both modules using [xcaddy](https://github.com/caddyserver/xcaddy):

```sh
xcaddy build --with github.com/doors-dev/doors-caddy/plugin
```

This imports the `plugin/` package which registers `doors_geo`, `doors_handler`,
and `doors_upstreams`.

## Configuration

### Caddyfile

```
example.com {
    doors_handler {
        secret <base64-aes-key>
        cookie_name upstream
        upstream {
            pod_cidr 10.0.0.0/24
            host svc.namespace.svc.cluster.local
            upstream_port 8080
        }
    }
    reverse_proxy {
        dynamic_upstreams doors_upstreams
    }
}
```

- `secret` — Base64-encoded AES key, shared with the Doors app (required).
- `cookie_name` — Name of the cookie carrying the encrypted pod IP
  (required when more than one upstream).
- `upstream` blocks:
  - `pod_cidr` — CIDR covering all pods on this server (required).
  - `host` — DNS name resolving to the fresh deployment (required).
  - `upstream_port` — Port the Doors app listens on (required).

For horizontal scaling, add more `upstream` blocks in `doors_handler`:

```
upstream {
    pod_cidr 10.0.1.0/24
    host svc-2.namespace.svc.cluster.local
    upstream_port 8080
}
```

`doors_upstreams` inside `reverse_proxy` takes no arguments — it reads
upstreams from the request context populated by `doors_handler`.

### Caddyfile — doors_geo

```
example.com {
    doors_geo {
        west.example.com {
            AG AI AR AW BB BL BM BO BQ BR
            BS BZ CA CK CL CO CR CU CW DM
            DO EC FK GD GF GL GP GT GY HN
            HT JM KN KY LC MF MH MQ MS MX
            NI NU PA PE PF PM PR PY SR SV
            SX TC TO TT US UY VC VE VG VI
            WS
        }
        central.example.com {
            AD AE AF AL AM AO AQ AT AX AZ
            BA BE BF BG BH BI BJ BY CD CF
            CG CH CI CM CV CY CZ DE DJ DK
            DZ EE EG ER ES ET EU FI FO FR
            GA GB GE GG GH GI GM GN GQ GR
            GW HR HU IE IL IM IQ IR IS IT
            JE JO KE KG KM KW KZ LB LI LR
            LS LT LU LV LY MA MC MD ME MG
            MK ML MR MT MU MW MZ NA NE NG
            NL NO OM PL PS PT QA RE RO RS
            RU RW SA SC SD SE SI SK SL SN
            SO SS ST SY SZ TD TG TJ TK TM
            TN TR UA UG UZ VA WF YE YT ZA
            ZM ZW ZZ
        }
        asia.example.com {
            AS AU BD BN BT CN FJ FM GU HK
            ID IN IO JP KH KI KP KR LA LK
            MM MN MO MP MV MY NC NF NP NR
            NZ PG PH PK PW SB SG TH TL TV
            TW VN VU
        }
    }
}
```

Each key that is not a recognised directive (`ipv4_url`, `ipv6_url`,
`update_interval`, `download_timeout`) is treated as a domain block.
The values inside are
[ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)
country codes — one or more per line.

| Directive | Default | Description |
|---|---|---|
| `ipv4_url` | `ipdeny.com/ipblocks/data/countries/all-zones.tar.gz` | URL for IPv4 country zone archive |
| `ipv6_url` | `ipdeny.com/ipv6/ipaddresses/blocks/ipv6-all-zones.tar.gz` | URL for IPv6 country zone archive |
| `update_interval` | `24h` | Interval between database downloads |
| `download_timeout` | `30s` | HTTP client timeout for downloads |
| `<domain>` | (required — at least one) | Target domain; block contains country codes to redirect |

### JSON

```json
{
    "apps": {
        "http": {
            "servers": {
                "example": {
                    "routes": [
                        {
                            "handle": [
                                {
                                    "handler": "doors_handler",
                                    "secret": "<base64-aes-key>",
                                    "cookie_name": "upstream",
                                    "upstreams": [
                                        {
                                            "cidr": "10.0.0.0/24",
                                            "host": "svc.namespace.svc.cluster.local",
                                            "port": 8080
                                        }
                                    ]
                                },
                                {
                                    "handler": "reverse_proxy",
                                    "upstreams": {
                                        "source": "doors_upstreams"
                                    }
                                }
                            ]
                        }
                    ]
                }
            }
        }
    }
}
```

### JSON — doors_geo

```json
{
    "handler": "doors_geo",
    "update_interval": "24h",
    "download_timeout": "30s",
    "redirects": {
        "west.example.com": [
            "AG", "AI", "AR", "AW", "BB", "BL", "BM", "BO", "BQ", "BR",
            "BS", "BZ", "CA", "CK", "CL", "CO", "CR", "CU", "CW", "DM",
            "DO", "EC", "FK", "GD", "GF", "GL", "GP", "GT", "GY", "HN",
            "HT", "JM", "KN", "KY", "LC", "MF", "MH", "MQ", "MS", "MX",
            "NI", "NU", "PA", "PE", "PF", "PM", "PR", "PY", "SR", "SV",
            "SX", "TC", "TO", "TT", "US", "UY", "VC", "VE", "VG", "VI",
            "WS"
        ],
        "central.example.com": [
            "AD", "AE", "AF", "AL", "AM", "AO", "AQ", "AT", "AX", "AZ",
            "BA", "BE", "BF", "BG", "BH", "BI", "BJ", "BY", "CD", "CF",
            "CG", "CH", "CI", "CM", "CV", "CY", "CZ", "DE", "DJ", "DK",
            "DZ", "EE", "EG", "ER", "ES", "ET", "EU", "FI", "FO", "FR",
            "GA", "GB", "GE", "GG", "GH", "GI", "GM", "GN", "GQ", "GR",
            "GW", "HR", "HU", "IE", "IL", "IM", "IQ", "IR", "IS", "IT",
            "JE", "JO", "KE", "KG", "KM", "KW", "KZ", "LB", "LI", "LR",
            "LS", "LT", "LU", "LV", "LY", "MA", "MC", "MD", "ME", "MG",
            "MK", "ML", "MR", "MT", "MU", "MW", "MZ", "NA", "NE", "NG",
            "NL", "NO", "OM", "PL", "PS", "PT", "QA", "RE", "RO", "RS",
            "RU", "RW", "SA", "SC", "SD", "SE", "SI", "SK", "SL", "SN",
            "SO", "SS", "ST", "SY", "SZ", "TD", "TG", "TJ", "TK", "TM",
            "TN", "TR", "UA", "UG", "UZ", "VA", "WF", "YE", "YT", "ZA",
            "ZM", "ZW", "ZZ"
        ],
        "asia.example.com": [
            "AS", "AU", "BD", "BN", "BT", "CN", "FJ", "FM", "GU", "HK",
            "ID", "IN", "IO", "JP", "KH", "KI", "KP", "KR", "LA", "LK",
            "MM", "MN", "MO", "MP", "MV", "MY", "NC", "NF", "NP", "NR",
            "NZ", "PG", "PH", "PK", "PW", "SB", "SG", "TH", "TL", "TV",
            "TW", "VN", "VU"
        ]
    }
}
```

JSON keys: `ipv4_url`, `ipv6_url`, `update_interval`, `download_timeout`,
`redirects` (object with string keys and arrays of two‑letter country codes).

## Token format

- **Plaintext**: raw pod IP bytes (`netip.Addr.AsSlice`)
- **Encryption**: AES-GCM with random nonce, AAD `doors-pod-ip-v1`
- **Encoding**: base64 raw-URL (no padding)

## Modules

| Package | Role |
|---------|------|
| `doorscaddy` (root) | Public API imported by Doors apps |
| `common/` | Shared token cipher and request‑context helpers |
| `plugin/` | Registration entry point for all three Caddy modules |
| `modules/handler/` | Caddy module `http.handlers.doors_handler` |
| `modules/upstream/` | Caddy module `http.reverse_proxy.upstreams.doors_upstreams` |
| `modules/geo/` | Caddy module `http.handlers.doors_geo` |

## License

Apache 2.0 — see [LICENSE](LICENSE).
