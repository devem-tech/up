# 🔄 up-to-date

`up-to-date` periodically scans running Docker containers, pulls their configured images, and recreates containers only when the image ID changes. It preserves the original container configuration, supports optional rolling updates for eligible containers, and lets you control exactly which containers are updated via labels.

✨ **Key features**
- 🏷️ Selective updates via labels
- 🔁 Smart change detection (recreate only on image ID change)
- ♻️ Rolling updates with healthcheck awareness
- 🧹 Optional cleanup of unused images
- 📋 Configurable logging

---

## ⚙️ How it works

- 🔍 Scans running containers
- ⬇️ Pulls the configured image (`repo:tag` or digest)
- 🔁 If the image ID changed:
  - ⛔ Stops the container
  - ♻️ Recreates it with the same config
  - ▶️ Starts the container
- 🔄 If rolling updates are enabled for a container:
  - ✅ Creates a new container first
  - 🩺 Waits for healthcheck (if configured)
  - ⛔ Stops and removes the old container
  - 🔁 Renames the new container to the original name
- 🧹 Optionally removes the previous image if it is no longer used

---

## 🏷️ Labels

To enable updates for a container, add a label (default selector below):

```yaml
devem.tech/up-to-date.enabled: "true"
```

Only containers with this label are managed when `--label-enable` is set.  
The selector can be changed with `--label`.

To enable rolling updates (create new, then stop old), add a label (default selector below):

```yaml
devem.tech/up-to-date.rolling: "true"
```

Rolling updates are only applied to containers with this label and without published ports or host networking.  
The selector can be changed with `--rolling-label`.

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
      - --docker-config=/config.json
```

---

## 🔧 Configuration flags

| Flag | Default | Description |
| --- | --- | --- |
| `--interval` | `30s` | How often to check for updates |
| `--cleanup` | `false` | Remove old images for updated containers |
| `--label-enable` | `false` | Update only containers that have label |
| `--label` | `devem.tech/up-to-date.enabled=true` | Label selector for `--label-enable` (key or key=value) |
| `--rolling-label` | `devem.tech/up-to-date.rolling=true` | Label selector to enable rolling updates (key or key=value) |
| `--docker-config` | empty | Path to `config.json` for registry auth (optional) |
| `--log-level` | `info` | Log level: `debug`, `info`, `warn`, `error` |

---

## 🔔 Telegram notifications

If `TELEGRAM_API_TOKEN` is set, `up-to-date` will send a Telegram message
when updates are applied or failures occur.

Required environment variables:

| Variable | Description |
| --- | --- |
| `TELEGRAM_API_TOKEN` | Bot token |
| `TELEGRAM_CHAT_ID` | Target chat ID |

---

## 🔐 Registry authentication

If your images are private, mount Docker's `config.json` and pass `--docker-config`.

---

## 📄 License

MIT
