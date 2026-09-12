# Dev Auth Compose Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a Docker Compose dev environment that runs four cetacean instances simultaneously — one per auth mode (none, OIDC, headers, cert) — for local auth testing.

**Architecture:** Five config files in `dev/auth/`: one compose file wiring 10 services (4 cetacean instances, 3 Caddy proxies, Dex, cert-init), three Caddyfiles, and a Dex config. cert-init generates mTLS certs at startup into a shared volume. Caddy handles TLS termination (OIDC) and mTLS client auth (cert mode).

**Tech Stack:** Docker Compose, Caddy 2, Dex (OIDC provider), OpenSSL (cert generation)

**Spec:** `docs/superpowers/specs/2026-03-15-dev-auth-compose-design.md`

---

## Chunk 1: All files

This is a single chunk — all files are interdependent config with no code to test.

### Task 1: Create Dex config

**Files:**
- Create: `dev/auth/dex.yaml`

- [ ] **Step 1: Generate bcrypt hash for dev password**

Run: `htpasswd -bnBC 10 "" password | tr -d ':'` or use a known hash.

Use pre-computed hash: `$2a$10$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4W` (bcrypt of "password").

- [ ] **Step 2: Create `dev/auth/dex.yaml`**

```yaml
issuer: http://dex:5556/dex

storage:
  type: sqlite3
  config:
    file: ":memory:"

web:
  http: 0.0.0.0:5556

staticClients:
  - id: cetacean
    secret: cetacean-dev-secret
    name: Cetacean Dev
    redirectURIs:
      - https://localhost:9002/auth/callback

enablePasswordDB: true

staticPasswords:
  - email: dev@localhost
    hash: "$2a$10$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4W"
    username: Dev User
    userID: "00000000-0000-0000-0000-000000000001"
```

### Task 2: Create Caddyfiles

**Files:**
- Create: `dev/auth/Caddyfile.oidc`
- Create: `dev/auth/Caddyfile.headers`
- Create: `dev/auth/Caddyfile.cert`

- [ ] **Step 1: Create `dev/auth/Caddyfile.oidc`**

```caddyfile
{
	local_certs
}

:9002 {
	tls internal
	reverse_proxy cetacean-oidc:9000
}
```

- [ ] **Step 2: Create `dev/auth/Caddyfile.headers`**

```caddyfile
:9003

reverse_proxy cetacean-headers:9000 {
	header_up X-Auth-User "dev@localhost"
	header_up X-Auth-Name "Dev User"
	header_up X-Auth-Email "dev@localhost"
	header_up X-Auth-Groups "admins,developers"
}
```

- [ ] **Step 3: Create `dev/auth/Caddyfile.cert`**

```caddyfile
:9004

reverse_proxy https://cetacean-cert:9000 {
	transport http {
		tls_client_auth /certs/client.pem /certs/client-key.pem
		tls_trusted_ca_certs /certs/ca.pem
	}
}
```

### Task 3: Create Docker Compose file

**Files:**
- Create: `dev/auth/compose.yaml`

- [ ] **Step 1: Create `dev/auth/compose.yaml`**

```yaml
services:
  # --- Certificate generation (one-shot) ---
  cert-init:
    image: alpine
    command:
      - sh
      - -c
      - |
        set -e
        apk add --no-cache openssl >/dev/null 2>&1
        cd /certs

        # CA
        openssl ecparam -genkey -name prime256v1 -out ca-key.pem
        openssl req -x509 -new -key ca-key.pem -out ca.pem -days 365 \
          -subj "/CN=Cetacean Dev CA"

        # Server cert (for cetacean-cert)
        openssl ecparam -genkey -name prime256v1 -out server-key.pem
        openssl req -new -key server-key.pem -out server.csr \
          -subj "/CN=cetacean-cert"
        openssl x509 -req -in server.csr -CA ca.pem -CAkey ca-key.pem \
          -CAcreateserial -out server.pem -days 365 \
          -extfile <(echo "subjectAltName=DNS:cetacean-cert")

        # Client cert (for caddy-cert to present)
        openssl ecparam -genkey -name prime256v1 -out client-key.pem
        openssl req -new -key client-key.pem -out client.csr \
          -subj "/CN=caddy-proxy"
        openssl x509 -req -in client.csr -CA ca.pem -CAkey ca-key.pem \
          -CAcreateserial -out client.pem -days 365

        rm -f *.csr *.srl
        echo "Certificates generated successfully"
    volumes:
      - certs:/certs

  # --- OIDC provider ---
  dex:
    image: dexidp/dex
    command: ["dex", "serve", "/etc/dex/dex.yaml"]
    ports:
      - "5556:5556"
    volumes:
      - ./dex.yaml:/etc/dex/dex.yaml:ro
    networks:
      - auth-dev

  # --- Auth mode: none (port 9001) ---
  cetacean-none:
    image: cetacean:latest
    ports:
      - "9001:9000"
    environment:
      CETACEAN_AUTH_MODE: none
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    networks:
      - auth-dev

  # --- Auth mode: oidc (port 9002 via caddy-oidc) ---
  cetacean-oidc:
    image: cetacean:latest
    environment:
      CETACEAN_AUTH_MODE: oidc
      CETACEAN_AUTH_OIDC_ISSUER: http://dex:5556/dex
      CETACEAN_AUTH_OIDC_CLIENT_ID: cetacean
      CETACEAN_AUTH_OIDC_CLIENT_SECRET: cetacean-dev-secret
      CETACEAN_AUTH_OIDC_REDIRECT_URL: https://localhost:9002/auth/callback
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    depends_on:
      - dex
    networks:
      - auth-dev

  caddy-oidc:
    image: caddy:2
    ports:
      - "9002:9002"
    volumes:
      - ./Caddyfile.oidc:/etc/caddy/Caddyfile:ro
    depends_on:
      - cetacean-oidc
    networks:
      - auth-dev

  # --- Auth mode: headers (port 9003 via caddy-headers) ---
  cetacean-headers:
    image: cetacean:latest
    environment:
      CETACEAN_AUTH_MODE: headers
      CETACEAN_AUTH_HEADERS_SUBJECT: X-Auth-User
      CETACEAN_AUTH_HEADERS_NAME: X-Auth-Name
      CETACEAN_AUTH_HEADERS_EMAIL: X-Auth-Email
      CETACEAN_AUTH_HEADERS_GROUPS: X-Auth-Groups
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    networks:
      - auth-dev

  caddy-headers:
    image: caddy:2
    ports:
      - "9003:9003"
    volumes:
      - ./Caddyfile.headers:/etc/caddy/Caddyfile:ro
    depends_on:
      - cetacean-headers
    networks:
      - auth-dev

  # --- Auth mode: cert (port 9004 via caddy-cert) ---
  cetacean-cert:
    image: cetacean:latest
    environment:
      CETACEAN_AUTH_MODE: cert
      CETACEAN_AUTH_CERT_CA: /certs/ca.pem
      CETACEAN_TLS_CERT: /certs/server.pem
      CETACEAN_TLS_KEY: /certs/server-key.pem
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - certs:/certs:ro
    depends_on:
      cert-init:
        condition: service_completed_successfully
    networks:
      - auth-dev

  caddy-cert:
    image: caddy:2
    ports:
      - "9004:9004"
    volumes:
      - ./Caddyfile.cert:/etc/caddy/Caddyfile:ro
      - certs:/certs:ro
    depends_on:
      cert-init:
        condition: service_completed_successfully
      cetacean-cert:
        condition: service_started
    networks:
      - auth-dev

networks:
  auth-dev:

volumes:
  certs:
```

### Task 4: Smoke test

- [ ] **Step 1: Verify /etc/hosts has `dex` entry**

Run: `grep -q 'dex' /etc/hosts && echo "OK" || echo "Add '127.0.0.1 dex' to /etc/hosts"`

- [ ] **Step 2: Build cetacean image**

Run: `cd /Users/moritz/GolandProjects/cetacean && docker build -t cetacean:latest .`

- [ ] **Step 3: Start the stack**

Run: `docker compose -f dev/auth/compose.yaml up -d`

- [ ] **Step 4: Verify all containers are running**

Run: `docker compose -f dev/auth/compose.yaml ps`

Expected: All services running (cert-init exited 0, rest running).

- [ ] **Step 5: Test each auth mode**

```bash
# No auth
curl -s http://localhost:9001/auth/whoami | jq .

# Headers (auto-authenticated)
curl -s http://localhost:9003/auth/whoami | jq .

# Cert (via Caddy mTLS)
curl -s http://localhost:9004/auth/whoami | jq .

# OIDC (should return 401 without session)
curl -sk https://localhost:9002/auth/whoami
```

- [ ] **Step 6: Commit**

```bash
git add dev/auth/
git commit -m "feat: add dev auth compose for local testing of all auth modes"
```
