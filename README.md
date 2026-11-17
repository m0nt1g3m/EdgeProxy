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
