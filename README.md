# EdgeProxy

EdgeProxy is a reverse-proxy manager for Docker-based deployments. It generates
nginx configuration, optionally requests a Let's Encrypt certificate, reloads
nginx automatically, and keeps every site's parameters editable.

It supports multiple upstreams, load balancing, passive health checks with
failover, timeouts, rate/concurrency limits, and an optional response cache.

## Requirements

- Docker
- Docker Compose
- Go 1.26+ (only to build from source)
- A Docker network named `proxy_net`
- An email for Let's Encrypt (`EMAIL`)

## Quick start

1. Create the environment and compose files:

   ```bash
   cp docker-compose.example.yml docker-compose.yml
   printf 'EMAIL=you@example.com\n' > .env
   ```

2. Create the shared Docker network if it does not exist:

   ```bash
   docker network create proxy_net
   ```

3. Start nginx and certbot:

   ```bash
   docker compose up -d
   ```

4. Run the app:

   ```bash
   go run ./cmd/app
   ```

   Or build and run the binary:

   ```bash
   go build -o edgeproxy ./cmd/app
   ./edgeproxy
   ```

## Interactive menu

Running without action flags opens a menu:

- **Add a new site** — domain, service type, upstreams, tuning, TLS.
- **Edit site parameters** — change anything without recreating the site.
- **Delete a site** — removes the nginx config and saved settings, keeps certificates.
- **List sites** — shows managed and imported sites.

The edit form is prefilled with the current values. If a generated config was
created by an older EdgeProxy version (or by hand using the same conventions),
it is imported automatically the first time you edit it.

## Managing sites from the command line

```bash
# List sites (managed + imported)
./edgeproxy -list

# Edit one parameter; all other settings are preserved
./edgeproxy -edit proxy.example.com -upstream app1:8080,app2:8080 -yes

# Switch to IP-hash balancing and raise the keepalive pool
./edgeproxy -edit proxy.example.com -balance ip_hash -keepalive 128 -yes

# Add rate limiting and a response cache
./edgeproxy -edit static.example.com -rate-limit 50 -rate-burst 100 -cache-ttl 5m -yes

# Delete a site (certificate files are kept)
./edgeproxy -delete old.example.com -yes
```

When `-edit` is used without tuning flags, the interactive edit form opens.
Unspecified flags keep their current values; specify `-ssl=false` explicitly to
disable TLS.

## Non-interactive mode

Pass `-domain` to skip the interactive forms:

```bash
# Reverse proxy with two backends and least-connections balancing
./edgeproxy -domain proxy.example.com -type proxy \
  -upstream app1:8080,app2:8080 -ssl -yes

# Streaming upstream (no buffering)
./edgeproxy -domain s3.example.com -type stream \
  -upstream 127.0.0.1:18080 -ssl -yes

# Redirect
./edgeproxy -domain www.example.com -type redirect \
  -redirect https://example.com -ssl -yes
```

Use `./edgeproxy -h` for every flag.

## Upstreams and load balancing

`-upstream` takes a comma-separated list. Each entry is `host:port` followed by
optional space-separated parameters:

| Parameter | Meaning | Default |
| --- | --- | --- |
| `weight=N` | Relative weight | `1` |
| `max_fails=N` | Failures before ejecting the server (`0` disables ejection) | `3` |
| `fail_timeout=T` | Ejection window | `10s` |
| `backup` | Only used when all primary servers are unavailable | off |
| `down` | Administratively disabled | off |

Example:

```bash
./edgeproxy -domain api.example.com -type proxy \
  -upstream 'app1:8080, app2:8080 weight=2 max_fails=5 fail_timeout=30s, 10.0.0.9:8080 backup' \
  -balance least_conn -ssl -yes
```

Balancing methods (`-balance`):

- `least_conn` — least active connections (default)
- `round_robin` — nginx default
- `ip_hash` — sticky sessions by client IP
- `hash_uri` — consistent hash of the request URI
- `random_two` — random choice between two least-loaded servers

`backup` servers are supported by nginx only with `round_robin` and
`least_conn`; EdgeProxy rejects other combinations instead of generating an
invalid config.

## Fault tolerance and high load

Every proxied site gets:

- Upstream keepalive pooling (`64` connections per worker by default).
- `proxy_next_upstream error timeout http_502 http_503 http_504` — requests
  are retried on another backend after connection errors, timeouts, or common
  gateway failures.
- `proxy_next_upstream_tries 3` and `proxy_next_upstream_timeout 10s` — bounded
  failover, so a request cannot hang indefinitely across dead backends.
- Passive health checks per backend (`max_fails` / `fail_timeout`).
- Short connect timeout (`5s`) so dead backends are detected quickly.
- TCP keepalive on upstream sockets.
- Configurable read/write timeouts and request body size.

Optional protections:

- `-rate-limit N` — requests per second per client IP (`limit_req`).
- `-rate-burst N` — burst allowance, defaults to `20` when a rate is set.
- `-conn-limit N` — concurrent connections per client IP (`limit_conn`).
- `-cache-ttl DURATION` — proxy response cache for `GET`/`HEAD` responses.

The response cache bypasses and avoids storing responses for requests carrying
`Authorization` or `Cookie` headers, and serves stale content when backends
fail. It is intended for public/static content; leave it off for APIs with
per-user data.

The base `nginx/nginx.conf` is also tuned for throughput: `worker_connections
16384`, large keepalive limits, open-file cache, header hash tuning, and
timeouts that release abandoned clients quickly.

### Parameter defaults

| Flag | Default |
| --- | --- |
| `-body-size` | `10M` (`0` for stream sites) |
| `-balance` | `least_conn` |
| `-keepalive` | `64` (`0` disables the pool) |
| `-connect-timeout` | `5s` |
| `-read-timeout` | `60s` (`3600s` for websocket/stream) |
| `-send-timeout` | `60s` |
| `-tries` | `3` |
| `-rate-limit` / `-rate-burst` / `-conn-limit` | `0` (disabled) |
| `-cache-ttl` | empty (disabled) |

## Loopback upstreams

The nginx container cannot reach `127.0.0.1` on the host: inside the container
that address points to nginx itself. EdgeProxy therefore rewrites loopback
upstreams (`127.0.0.1`, `localhost`, `[::1]`) to `host.docker.internal` and
starts a small `edgeproxy-hostrelay-<port>` container. The relay runs in the
host network namespace, listens on the Docker host gateway, and forwards to the
loopback service. The upstream service stays bound to `127.0.0.1`.

If the host runs UFW, allow the `proxy_net` subnet to reach those relay ports:

```bash
SUBNET=$(docker network inspect proxy_net --format '{{(index .IPAM.Config 0).Subnet}}')
sudo ufw allow from "$SUBNET" to any port 8080 proto tcp
```

Relays that are no longer referenced by any generated site config are removed
after a successful edit or delete.

## Saved settings and editable parameters

Every site is saved as JSON in `sites/<primary-domain>.json`. The file stores
the domains, service type, upstreams, balancing method, timeouts, limits, cache
settings, and TLS choice. It is used to prefill the edit form and to preserve
parameters when only one flag is changed.

Generated nginx configs remain plain text in `nginx/conf.d/<primary-domain>.conf`.
You can inspect them or keep them under version control, but edit parameters
through EdgeProxy so the saved settings stay in sync.

## Main files

- `docker-compose.example.yml` — nginx + certbot setup
- `nginx/nginx.conf` — global nginx tuning
- `nginx/conf.d/` — generated site configs
- `sites/` — saved site settings (edit through the app)
- `certbot/` — certificates and ACME challenge data

## Run via Task

```bash
task setup
# or only build and run the app

task run
# or

task build
./edgeproxy
```

## Manual run without Task

```bash
cp docker-compose.example.yml docker-compose.yml
printf 'EMAIL=you@example.com\n' > .env
docker network inspect proxy_net >/dev/null 2>&1 || docker network create proxy_net
docker compose up -d
go run ./cmd/app
```

Or build and run directly:

```bash
go build -o edgeproxy ./cmd/app
./edgeproxy
```

## Notes

- The app reads `.env` from the current working directory.
- `EMAIL` is required for certbot.
- Run the app from the project root so generated config paths and Docker mounts match the expected layout.
- Changing the domain list on an SSL site refreshes the certificate with the new
  names (`certbot --expand`).
- Active health checks (`health_check`) are an NGINX Plus feature; this project
  uses nginx's passive failure detection and bounded retries.
