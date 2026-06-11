# doors-caddy

Caddy v2 upstream source for zero-interruption rolling deployments and load
balancing of [Doors](https://github.com/doors-dev/doors) apps.

## Features

- **Zero-interruption rollouts** — draining pods stay reachable for existing
  sessions until they naturally end.
- **Load balancing** — distribute new sessions across servers with
  cookie-based server affinity.
- **Pod-level routing** — Doors system requests (`/~/*`) reach the exact
  pod that owns the session, even mid-rollout.
- **Static Caddy config** — hosts resolve to fresh deployments
  automatically; no config changes needed during a rollout.

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
The plugin decrypts it, matches the IP against upstream CIDRs, and dials
the pod directly. This guarantees system calls always reach the instance
that owns the session — whether the pod is fresh or draining.

**Normal requests, single upstream**. Always route to the host. The host
resolves to the fresh deployment.

**Normal requests, multiple upstreams**. Read the `upstream` cookie
(encrypted pod IP, set by Doors via `ServerIDCookieName`). Match the IP
against upstream CIDRs to keep the session on the same server. No cookie
means a new session — Caddy's load-balancing policy selects among all
upstreams.

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

## Configuration

### Caddyfile

```
example.com {
    reverse_proxy {
        dynamic_upstreams doors_upstream {
            secret <base64-aes-key>
            cookie_name upstream
            upstream {
                pod_cidr 10.0.0.0/24
                host svc.namespace.svc.cluster.local
                upstream_port 8080
            }
        }
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

For horizontal scaling, add more `upstream` blocks:

```
upstream {
    pod_cidr 10.0.1.0/24
    host svc-2.namespace.svc.cluster.local
    upstream_port 8080
}
```

### JSON

```json
{
    "handler": "reverse_proxy",
    "upstreams": {
        "source": "doors_upstream",
        "secret": "<base64-aes-key>",
        "cookie_name": "upstream",
        "upstreams": [
            {
                "cidr": "10.0.0.0/24",
                "host": "svc.namespace.svc.cluster.local",
                "port": 8080
            }
        ]
    }
}
```

## Token format

- **Plaintext**: raw pod IP bytes (`netip.Addr.AsSlice`)
- **Encryption**: AES-GCM with random nonce, AAD `doors-pod-ip-v1`
- **Encoding**: base64 raw-URL (no padding)

## Packages

| Package | Role |
|---------|------|
| `doorscaddy` (root) | Public API imported by Doors apps |
| `lib/` | Internal AES-GCM cipher implementation |
| `upstream/` | Caddy v2 module (`http.reverse_proxy.upstreams.doors_upstream`) |

## License

Apache 2.0 — see [LICENSE](LICENSE).
