# Hourglass — Self-Hosted Crontab Manager

**Hourglass** is a lightweight, self-hosted web UI for managing Linux cron jobs. See your scheduled tasks, view execution history, and add/edit/delete jobs without touching the command line.

## Features

- 📋 **View all cron jobs** in a clean table interface
- ⏱️ **See when each job last ran** (tracked automatically by Hourglass)
- ✅ **View exit status** — success or failure at a glance
- ✏️ **Add/edit/delete jobs** through the web UI
- 🌐 **Connection manager** — manage multiple Hourglass instances from one place
- 🤖 **MCP server** — let AI agents (Claude Desktop, Claude Code, etc.) list, create, edit, and delete cron jobs
- 🔒 **No external dependencies** — single binary, runs anywhere
- 🚀 **Auto-tracked execution history** — no setup required
- 📱 **Responsive design** — works on desktop and mobile

## Requirements

- **Linux** (20.04 LTS or newer) or **macOS** (12 Monterey or newer)
- `crontab` installed (included by default on both platforms)
- Optional: Basic auth credentials (for remote access)

## Installation

### Homebrew (macOS)

```bash
brew install TillmanBuildsTech/tap/hourglass
```

This builds Hourglass from source on your machine (Homebrew installs Go and
Node as temporary build dependencies). It only takes a few seconds, and
because the binary is compiled locally rather than downloaded, macOS
Gatekeeper won't flag it with a security warning.

### Download Binary

Download the latest binary for your platform from [GitHub Releases](https://github.com/TillmanBuildsTech/hourglass/releases):

```bash
wget https://github.com/TillmanBuildsTech/hourglass/releases/download/v1.0.0/hourglass-linux-amd64
chmod +x hourglass-linux-amd64
sudo mv hourglass-linux-amd64 /usr/local/bin/hourglass
```

### macOS

Download the darwin binary:

```bash
curl -L https://github.com/TillmanBuildsTech/hourglass/releases/download/v0.2.0/hourglass-darwin-amd64 -o hourglass
chmod +x hourglass
sudo mv hourglass /usr/local/bin/
```

Or for Apple Silicon:

```bash
curl -L https://github.com/TillmanBuildsTech/hourglass/releases/download/v0.2.0/hourglass-darwin-arm64 -o hourglass
chmod +x hourglass
sudo mv hourglass /usr/local/bin/
```

**Note:** macOS may prompt for Full Disk Access the first time Hourglass reads or writes the crontab. Grant access in System Settings → Privacy & Security → Full Disk Access if prompted.

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

### Install as a Service (Linux — .deb / .rpm)

Debian/Ubuntu and Fedora/RHEL packages ship a systemd unit and enable the
service automatically. On first install a random password is generated and
printed, and the service starts bound to `0.0.0.0:8080` with mDNS
advertisement, so it is immediately reachable from any device on your LAN:

```
http://hourglass.local:8080     # from any device on the network
http://<this-host>:8080         # from the host itself
```

```bash
# Debian / Ubuntu
sudo dpkg -i hourglass_linux_amd64.deb

# Fedora / RHEL / openSUSE
sudo rpm -i hourglass_linux_amd64.rpm
```

Credentials are saved in `/etc/hourglass.env` (chmod 600) — edit and
`sudo systemctl restart hourglass` to change them. The service installs the
unit to `/usr/lib/systemd/system/hourglass.service`.

The service runs as **root** by default (it manages root's crontab). To run it
as another user instead — managing *that* user's crontab and `~/.ssh` — create
an override drop-in rather than editing the packaged unit (so it survives
upgrades):

```bash
sudo mkdir -p /etc/systemd/system/hourglass.service.d
printf '[Service]\nUser=someuser\nGroup=someuser\n' \
  | sudo tee /etc/systemd/system/hourglass.service.d/user.conf
sudo systemctl daemon-reload && sudo systemctl restart hourglass
```

The `curl | sh` installer also accepts `HOURGLASS_USER=someuser` to do this for
you; the `.deb`/`.rpm` postinstall honors it when passed to the package manager.

For distros without packages:

```bash
curl -fsSL https://github.com/TillmanBuildsTech/hourglass/releases/latest/download/install.sh | sh
```

### Install as a Service (macOS — Homebrew)

```bash
brew install TillmanBuildsTech/tap/hourglass
brew services start hourglass
```

The formula runs Hourglass as a launchd agent (`keep_alive`, logs in
`$(brew --prefix)/var/log/hourglass.log`). It binds `127.0.0.1:8080` by
default — safe, no credentials needed. To expose it on your LAN at
`http://hourglass.local:8080`, set `HOURGLASS_BIND=0.0.0.0:8080` plus
`HOURGLASS_AUTH_USER`/`HOURGLASS_AUTH_PASS` in
`~/Library/LaunchAgents/homebrew.mxcl.hourglass.plist`, then
`brew services restart hourglass` (Hourglass refuses to serve on
non-loopback binds without credentials).

### LAN access & mDNS (both platforms)

With `HOURGLASS_BIND=0.0.0.0:8080` and credentials set, Hourglass advertises
itself over mDNS (Bonjour) so `http://hourglass.local:8080` resolves from any
device on the network. The advertised name defaults to `hourglass`
(`HOURGLASS_MDNS_NAME` to override); disable with `HOURGLASS_MDNS=0`.

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

## MCP Server

Hourglass can run as a [Model Context Protocol](https://modelcontextprotocol.io) server over stdio, so AI agents can list, create, edit, and delete cron jobs directly:

```bash
./hourglass --mcp
```

This starts Hourglass in stdio JSON-RPC mode instead of the web UI — it's meant to be spawned as a subprocess by an MCP client, not run interactively. It operates on the local crontab (the same one `./hourglass` manages by default) and exposes these tools:

| Tool | What it does |
|------|--------------|
| `list_cron_jobs` | List all jobs with their index, schedule, command, comment, active state, and last run result |
| `create_cron_job` | Add a new job (`schedule`, `command`, optional `comment`) |
| `update_cron_job` | Replace the job at `index` with a new `schedule`/`command`/`comment`/`inactive` |
| `delete_cron_job` | Delete the job at `index` |
| `validate_cron_schedule` | Check a 5-field schedule string without writing anything |

Indices come from `list_cron_jobs` and can shift after any add/delete, so agents should re-list before acting on a stale index.

### Claude Desktop / Claude Code

Add Hourglass to your MCP client's config (e.g. `claude_desktop_config.json`, or via `claude mcp add`):

```json
{
  "mcpServers": {
    "hourglass": {
      "command": "/usr/local/bin/hourglass",
      "args": ["--mcp"]
    }
  }
}
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
| `HOURGLASS_AUTH_USER` | (none) | Basic auth username. **Required for non-loopback binds** |
| `HOURGLASS_AUTH_PASS` | (none) | Basic auth password. **Required for non-loopback binds** |
| `HOURGLASS_ALLOW_INSECURE` | (none) | `1` serves without auth on non-loopback binds (dangerous — explicit opt-in only) |
| `HOURGLASS_MDNS` | `1` | Advertise `hourglass.local` over mDNS/Bonjour (skipped on loopback binds; `0` disables) |
| `HOURGLASS_MDNS_NAME` | `hourglass` | mDNS hostname (the `hourglass` in `hourglass.local`) |

> **Security:** The web UI can execute arbitrary shell commands (Run now,
> cron writes). Hourglass **refuses to start** on a non-loopback bind
> (`0.0.0.0` or a LAN IP) unless `HOURGLASS_AUTH_USER` and
> `HOURGLASS_AUTH_PASS` are both set — an unauthenticated LAN-accessible
> instance is a remote code execution hole. Bind to `127.0.0.1` to skip
> credentials, or set `HOURGLASS_ALLOW_INSECURE=1` to explicitly opt out.

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

### "Operation not permitted" on macOS

macOS requires Full Disk Access for crontab operations.

**Solution:** Grant Full Disk Access to your terminal app (or to the `cron` binary):
1. Open System Settings → Privacy & Security → Full Disk Access
2. Add your terminal application (Terminal.app, iTerm2, etc.)
3. Restart Hourglass

## Limitations

- **Single Admin** — Only one person should edit jobs at a time. Concurrent edits may overwrite each other.
- **Launchd Not Supported** — Hourglass manages crontab jobs (which macOS still supports). Native `launchd` integration is planned for a future release.
- **Windows Not Supported** — Windows has no `crontab`; not currently planned.
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
