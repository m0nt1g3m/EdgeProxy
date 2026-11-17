# EdgeProxy

EdgeProxy is a small interactive reverse-proxy manager for Docker-based deployments. It generates nginx config, optionally requests a Let's Encrypt certificate, and reloads nginx automatically.

## Requirements

- Docker
- Docker Compose
- Go 1.26+
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

## What it does

The app asks for:

- domain or domains
- proxy type (`proxy`, `websocket`, `stream`, `redirect`)
- upstream target (`host:port`)
- optional HTTPS / Let's Encrypt certificate

Then it writes a config into `nginx/conf.d/` and reloads nginx. Certificates are stored under `certbot/conf`.

## Non-interactive mode

Pass `-domain` to skip the interactive form. This is useful for scripts and for
re-adding domains after a server rebuild:

```bash
# Normal reverse proxy
./edgeproxy -domain proxy.example.com -type proxy -upstream 127.0.0.1:8080 -ssl -yes

# Streaming upstream (disables proxy buffering, like Caddy's flush_interval -1)
./edgeproxy -domain s3.example.com -type stream -upstream 127.0.0.1:18080 -ssl -yes

# Redirect
./edgeproxy -domain www.example.com -type redirect -redirect https://example.com -ssl -yes
```

Use `./edgeproxy -h` for all flags.

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

## Main files

- `docker-compose.example.yml` — nginx + certbot setup
- `nginx/nginx.conf` — nginx base config
- `nginx/conf.d/` — generated site config files
- `certbot/` — certificate and ACME challenge data

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
