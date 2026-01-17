# 🔄 up-to-date

Automatically keep your Docker containers updated with the latest images from registries. `up-to-date` periodically checks for new image versions and updates containers only when the image actually changes, preserving all container configuration and avoiding unnecessary restarts. Unlike aggressive auto-update solutions, it gives you full control over which containers get updated.

✨ **Key features**
- 🏷️ Selective updates
- 🔁 Smart change detection
- 🧹 Automatic cleanup

---

## ⚙️ How it works

- 🔍 Scans running containers
- ⬇️ Pulls the configured image (`repo:tag` or digest)
- 🔁 If the image ID changed:
  - ⛔ Stops the container
  - ♻️ Recreates it with the same config
  - ▶️ Starts the container
- 🧹 Optionally removes the previous image if it is no longer used

---

## 🏷️ Labels

To enable updates for a container, add a label:

```yaml
devem.tech/up-to-date.enabled: "true"
```

Only containers with this label are managed when `--label-enable` is set.

To enable rolling updates (create new, then stop old), add a label:

```yaml
devem.tech/up-to-date.rolling: "true"
```

Rolling updates are only applied to containers with this label and without published ports.

---

## 🐳 Usage with Docker Compose

```yaml
services:
  up-to-date:
    image: ghcr.io/devem-tech/up-to-date:latest
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - /root/.docker/config.json:/config.json:ro
    command:
      - --interval=30s
      - --label-enable
      - --cleanup
      - --label=devem.tech/up-to-date.enabled=true
      - --docker-config=/config.json
```

---

## 🔧 Configuration flags

- `--interval` — how often to check for updates (default: `30s`)
- `--label-enable` — update only labeled containers
- `--label` — label selector to match (key or key=value)
- `--cleanup` — remove the old image after a successful update
- `--docker-config` — path to `config.json` for registry authentication
- `--rolling-label` — label selector to enable rolling updates (key or key=value)

---

## 🔐 Registry authentication

If your images are private, mount Docker's `config.json` and pass `--docker-config`.

---

## 📄 License

MIT
