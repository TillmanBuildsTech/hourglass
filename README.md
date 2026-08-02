# Hourglass — Self-Hosted Crontab Manager

**Hourglass** is a lightweight, self-hosted web UI for managing Linux cron jobs. See your scheduled tasks, view execution history, and add/edit/delete jobs without touching the command line.

## Features

- 📋 **View all cron jobs** in a clean table interface
- ⏱️ **See when each job last ran** (tracked automatically by Hourglass)
- ✅ **View exit status** — success or failure at a glance
- ✏️ **Add/edit/delete jobs** through the web UI
- 🌐 **Connection manager** — manage multiple Hourglass instances from one place
- 🔒 **No external dependencies** — single binary, runs anywhere
- 🚀 **Auto-tracked execution history** — no setup required
- 📱 **Responsive design** — works on desktop and mobile

## Requirements

- **Linux** (20.04 LTS or newer) with GNU coreutils (`date`, `printf`) — the default on all mainstream distros
- **crontab** installed
- Optional: Basic auth credentials (for remote access)

## Installation

### Download Binary

Download the latest binary for your platform from [GitHub Releases](https://github.com/TillmanBuildsTech/hourglass/releases):

```bash
wget https://github.com/TillmanBuildsTech/hourglass/releases/download/v1.0.0/hourglass-linux-amd64
chmod +x hourglass-linux-amd64
sudo mv hourglass-linux-amd64 /usr/local/bin/hourglass
```

### Or Build from Source

Requires Go 1.21+:

```bash
git clone https://github.com/brandontillman/hourglass.git
cd hourglass
go build -o hourglass .
sudo mv hourglass /usr/local/bin/
```

## Usage

### Local Development

```bash
./hourglass
# Opens on http://localhost:8080
```

### Remote Server

```bash
HOURGLASS_BIND=0.0.0.0:8080 \
HOURGLASS_AUTH_USER=admin \
HOURGLASS_AUTH_PASS=secretpass \
./hourglass
```

Then access at `http://your-server:8080` with basic auth.

### Run as Systemd Service

Create `/etc/systemd/system/hourglass.service`:

```ini
[Unit]
Description=Hourglass Crontab Manager
After=network.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/hourglass
Restart=always
RestartSec=10
Environment="HOURGLASS_BIND=127.0.0.1:8080"

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable hourglass
sudo systemctl start hourglass
```

Check status:

```bash
sudo systemctl status hourglass
sudo systemctl logs -u hourglass -f
```

## Connection Manager

Hourglass includes a built-in connection manager for accessing multiple Hourglass instances from a single UI.

### For Local-Only Instances

When Hourglass is running on `127.0.0.1` or `localhost`, the connection manager only shows the local connection and prevents adding remote connections. This is the default for local development or SSH tunnel access (see below).

### For Remote-Capable Instances

When Hourglass is bound to `0.0.0.0` or a specific hostname, you can add and manage multiple server connections:

1. Click "Manage Connections" in the top-right corner
2. Click "+ Add Connection"
3. Enter the hostname/IP, port, and optional label
4. Click "Save" to store the connection
5. Use "Connect" to switch between saved connections

**Note:** Only one connection can be active at a time. The UI will reload when you switch connections.

### SSH Tunneling for Remote Access

For secure remote access, use SSH port forwarding. This is the recommended approach for accessing Hourglass from outside your network:

```bash
ssh -L 8080:localhost:8080 user@remote-server
```

Then visit `http://localhost:8080` on your local machine.

**Advantages:**
- Secure encrypted connection
- No need to expose Hourglass to the internet
- Can run Hourglass on localhost without remote binding
- Works with key-based authentication

**For systemd-managed instances:**

Create an SSH tunnel helper script:

```bash
#!/bin/bash
ssh -L 8080:localhost:8080 user@remote-server
```

Then use it in your local shell profile or automation.

**With Authentication:**

If you've configured basic auth on Hourglass:

```bash
ssh -L 8080:localhost:8080 user@remote-server
# Visit http://localhost:8080 and enter credentials
```

## Configuration

### Checking the Version

The running version is shown in the web UI header, and available via:

```bash
./hourglass --version
curl http://localhost:8080/api/version
```

### Environment Variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `HOURGLASS_BIND` | `127.0.0.1:8080` | Server bind address |
| `HOURGLASS_AUTH_USER` | (none) | Basic auth username (optional) |
| `HOURGLASS_AUTH_PASS` | (none) | Basic auth password (optional) |

### Running as Non-Root

Hourglass must run as a user that can read the system crontab. On Linux, add your user to the `root` group or run as root:

```bash
sudo usermod -aG root $USER
newgrp root
./hourglass
```

Or run with sudo:

```bash
sudo ./hourglass
```

## Troubleshooting

### "Cannot read crontab: permission denied"

The running user doesn't have crontab permissions.

**Solution:** Run as root or add user to root group:
```bash
sudo hourglass
# or
sudo usermod -aG root $USER
newgrp root
hourglass
```

### Last run / status never shows up

Hourglass tracks execution history by wrapping each job's command so it logs its exit code and timestamp to `~/.hourglass/history.log` (the home directory of whichever user's crontab is being managed — the local user, or the SSH-remote user). If that user has no writable `$HOME`, history silently stays empty. Jobs added outside Hourglass (directly via `crontab -e`) aren't wrapped either, so they won't show history until edited and saved through Hourglass.

**Solution:** Make sure the crontab owner has a writable home directory.

### "No crontab found"

The system doesn't have any cron jobs yet.

**Solution:** Create one in Hourglass, or via crontab:
```bash
crontab -e
```

### "Address already in use :8080"

Another service is using port 8080.

**Solution:** Use a different port:
```bash
HOURGLASS_BIND=127.0.0.1:9000 ./hourglass
```

### UI not updating or showing "Loading..."

Check if the backend API is running. Open browser DevTools (F12) and check the Network tab for failed `/api/cron` requests.

**Solution:** Restart Hourglass and check logs.

## Limitations

- **Single Admin** — Only one person should edit jobs at a time. Concurrent edits may overwrite each other.
- **Linux Only** — macOS and Windows support deferred. Requires GNU `date` for millisecond timestamps.
- **Hourglass-Managed Jobs Only** — Only jobs added/edited through Hourglass are wrapped to record execution history. Jobs added directly via `crontab -e` won't show a last-run status until saved through Hourglass.
- **History Shows Latest Run Only** — Hourglass keeps the most recent execution per job, not a full history log (the `~/.hourglass/history.log` file itself does accumulate every run until you rotate or clear it).
- **No Clustering** — Each instance manages its own crontab.

## Architecture

**No External Dependencies** — Hourglass uses only Go stdlib:

- **HTTP Server:** `net/http`
- **Crontab I/O:** `os/exec` (runs `crontab` command)
- **Execution History:** Each managed job's command is wrapped so it appends its exit code and timestamp to `~/.hourglass/history.log`; Hourglass reads that file back (via the same local/SSH executor used for crontab I/O) instead of parsing system logs, since cron itself never reports exit codes to syslog.
- **UI:** Single HTML file with inline CSS (Tailwind CDN) and vanilla JavaScript

See [Design.md](Design.md) for detailed architecture decisions.

## Development

See [CLAUDE.md](CLAUDE.md) for development setup and contribution guidelines.

## License

MIT License — See LICENSE file for details.

## Contributing

Contributions welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Write tests for new features
4. Ensure all tests pass: `go test ./...`
5. Submit a pull request

## Support

Found a bug? Have a feature request? Open an issue on GitHub:
https://github.com/TillmanBuildsTech/hourglass/issues

---

**Built with Go • Single Binary • Zero Setup**
