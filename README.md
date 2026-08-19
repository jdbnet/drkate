# DrKate

Kubernetes disaster recovery platform. DrKate scrapes namespaced resources from a source cluster, stores them as encrypted YAML locally, and lets you view, edit, and deploy them to a DR cluster via a web UI.

Named DrKate because Kubernetes is often called k8s, which sounds like Kate.

## Features

- Scrape all namespaced resources from a source cluster (kubeconfig)
- Flexible namespace selection: whitelist, blacklist, or all with exclusions
- Manifest sanitization removes cluster noise before storage
- AES-256-GCM encrypted storage at rest
- Multi-user auth with roles (admin, operator, viewer)
- Vue + Tailwind dashboard showing DR sync status per namespace
- Monaco YAML editor with save and deploy actions

## Requirements

- Go 1.26+
- Node.js 20+ (for frontend build)
- Valid kubeconfig files for source and DR clusters

## Quick Start

1. Copy and edit config:

```bash
cp config.example.yaml config.yaml
```

2. Set secrets in `config.yaml` (or use `session_secret_env` / `encryption_key_env` to read from the environment):

```yaml
server:
  session_secret: "<64 hex chars from openssl rand -hex 32>"
storage:
  encryption_key: "<64 hex chars from openssl rand -hex 32>"
```

Generate values:

```bash
openssl rand -hex 32
```

For first-time setup, also set a bootstrap admin password:

```bash
export DRKATE_BOOTSTRAP_PASSWORD=your-admin-password
```

3. Build and run:

```bash
./build.sh
./drkate --config config.yaml
```

4. Open http://localhost:8080 and log in with username `admin` and your bootstrap password.

## Development

```bash
# Backend (build frontend first with ./build.sh)
./drkate --config config.yaml

# Frontend dev server (proxies /api to :8080)
cd web && npm run dev
```

## Configuration

See [config.example.yaml](config.example.yaml) for namespace modes, scrape options, and cluster kubeconfig paths.

## License

MIT
