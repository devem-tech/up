# 🚀 up

`up` is a minimal Docker container auto-updater inspired by watchtower.

It periodically checks running containers, pulls their images, and recreates containers
when a new image version is available.

✨ **Key features**
- 🏷️ Updates only labeled containers
- 🧹 Safe image cleanup (removes only the old image of an updated container)
- 🐳 Designed to run locally or inside Docker
- 🔒 Predictable and minimal behavior

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
devem.tech/up.enabled: "true"
```

Only containers with this label are managed when `--label-enable` is set.

---

## 🐳 Usage with Docker Compose

```yaml
services:
  up:
    image: ghcr.io/devem-tech/up:latest
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - /root/.docker/config.json:/config.json:ro
    command:
      - --interval=30s
      - --label-enable
      - --cleanup
      - --label-key=devem.tech/up.enabled
      - --label-value=true
      - --docker-config=/config.json
```

---

## 🔧 Configuration flags

- `--interval` — how often to check for updates (default: `30s`)
- `--label-enable` — update only labeled containers
- `--label-key` — label key to match
- `--label-value` — label value to match
- `--cleanup` — remove the old image after a successful update
- `--docker-config` — path to `config.json` for registry authentication

---

## 🔐 Registry authentication

If your images are private, mount Docker's `config.json` and pass `--docker-config`.
If the file is missing or invalid, `up` will continue without registry auth.

---

## 📄 License

MIT
