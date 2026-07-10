# VFD Control Server v3.8.1

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.23.2-blue?logo=go" alt="Go version" />
  <img src="https://img.shields.io/badge/Platform-Ubuntu%2024.04-orange?logo=ubuntu" alt="Ubuntu" />
  <img src="https://img.shields.io/badge/Status-Production-green" alt="Production Status" />
  <img src="https://img.shields.io/badge/License-MIT-brightgreen" alt="License" />
</p>

<p align="center">
  <b>Modern, real-time web-based control and monitoring for industrial Variable Frequency Drives (VFDs).</b><br>
  <i>Supports Invertek OptidriveP2 and OptidriveE3, WEG CFW500, and Automation Direct GS4-4020 drives.</i>
</p>

---

## 🚀 Features

- ⚡ **Persistent VFD Connections**  
  Each VFD is managed by exactly one dedicated connection goroutine with a persistent TCP connection for fast, reliable control and status updates. 
  Automatic reconnection, health monitoring, and clean socket teardown on connection loss.
- 🔒 **Race-Free Concurrency**  
  All shared state (drive data, connections, disabled drives, event log) is protected by mutexes or atomics; 
  poll cycles are serialized so live data can never be overwritten by a stale snapshot.
- ✅ **Unit Tests**  
  Core logic (frequency conversion formulas, drive status decoding, group filtering) is covered by `go test`.
- 🌐 **WebSocket-Powered UI**  
  Real-time updates and control via a modern, mobile-friendly web interface. 
  No page reloads required; all data is live.
- 🌀 **Group and Fan Management**  
  Organize VFDs into logical groups (e.g., "pods"). 
  Control individual fans or entire groups with one click.
- 🧩 **Drive Profiles**  
  Modular support for different drive types via `drive_profiles.json`. 
  Easily extendable for new drive models.
- 📊 **Prometheus Metrics**  
  Exposes `/metrics` endpoint for Prometheus monitoring and alerting.
- **System Status API**  
  New `/api/status` endpoint provides real-time system health, loading states, and connection metrics for external monitoring.
- **Control Events Log**  
  View recent control actions and their results in the UI.
- 🌙 **Dark Mode**  
  Toggleable dark/light mode for the web UI. 
- 🛠️ **Configurable via JSON**  
  All drives and profiles are configured via JSON files in `/etc/vfd`.
- 🛡️ **Security-Ready**  
  Designed to be run behind a reverse proxy for authentication and HTTPS.

---

## 🗂️ File Structure

```
📁 vfdserver/
 ├── vfdserver.go              # Main Go server source code
 ├── vfdserver_test.go         # Unit tests (freq-calc, status decoding, group filtering)
 ├── config.json               # Example configuration file for VFDs
 ├── drive_profiles.json       # Example drive type profiles
 ├── index.html                # Web UI served by the Go backend
 └── vfdserver-supervisord.conf # Example supervisord config
```

## ⚙️ Configuration

### 1️⃣ `/etc/vfd/config.json`

Defines the site, network, and all VFDs to be managed. 🏭

```json
{
  "SiteName": "BLU02",
  "BindIP": "10.33.10.53",
  "GroupLabel": "POD",
  "VFDs": [
    {
      "IP": "10.33.30.11",
      "Port": 502,
      "Unit": 1,
      "FanNumber": 1,
      "FanDesc": "1x 1800RPM 29.5kCFM",
      "Group": 1,
      "RpmHz": 30,
      "CfmRpm": 16.38888888,
      "DriveType": "OptidriveE3"
    }
    // ... more VFDs ...
  ]
}
```

- 🏷️ `SiteName`: Displayed in the UI and logs.
- 🌐 `BindIP`: IP address to bind the web server (use `0.0.0.0` for all interfaces).
- 🏷️ `GroupLabel`: Label for groups (e.g., "POD", "Zone").
- 🛠️ `VFDs`: List of VFDs, each with:
  - `IP`, `Port`, `Unit`, `FanNumber`, `FanDesc`, `Group`, `RpmHz`, `CfmRpm`, `DriveType`

### 2️⃣ `/etc/vfd/drive_profiles.json`

Defines register mappings and control logic for each supported drive type. ⚡

```json
{
  "OptidriveP2": {
    "Setpoint": [1, 207],
    "SpeedPresetMultiplier": 6,
    "Control": 0,
    "StartValue": 1,
    "StopValue": 0,
    "UnTripRegister": 0,
    "UnTripValue": 4,
    "OutputFrequency": 7,
    "OutputCurrent": 8,
    "Status": 6,
    "StatusBits": {
      "Enabled": 0,
      "Tripped": 1,
      "Inhibited": 3
    }
  },
  "OptidriveE3": {
    "Setpoint": [1],
    "Control": 0,
    "StartValue": 1,
    "StopValue": 0,
    "UnTripRegister": 0,
    "UnTripValue": 4,
    "OutputFrequency": 7,
    "OutputCurrent": 8,
    "Status": 6,
    "StatusBits": {
      "Enabled": 0,
      "Tripped": 1
    }
  }
}
```

> 🧩 **Tip:** Each key is a drive type (must match `DriveType` in config.json). Register addresses and control values are specific to your hardware.

---

## 🖥️ Web Interface

- 🟢 **Live Dashboard:** View all VFDs, grouped, with real-time status (speed, CFM, amps, etc.)
- 🎛️ **Control Panel:** Set speed (Hz or %), start/stop/hold fans, select all, and group quick controls
- 📋 **Event Log:** See recent control actions and their results
- 🌗 **Dark Mode:** Toggle with the button in the top right
- 📱 **Responsive:** Works on desktop and mobile

<p align="center">
  <img src="https://user-images.githubusercontent.com/991078/273420124-2e7e7e7e-2e7e-4e7e-8e7e-2e7e7e7e7e7e.png" width="600" alt="VFD Web UI Screenshot" />
</p>

---

## 🌍 Remote Control API

Remotely control the VFD server by sending JSON commands to its HTTP endpoints. All endpoints are accessible on `http://<BindIP>:80` (as set in your config). 🌐

### 🖥️ `/api/devices` (GET)

Fetch a list of all drives, including their configuration (from config) and current live data (setpoint, speed, rpm, cfm, amps, status, etc.).

**Example:**
```bash
curl http://10.33.10.53/api/devices
```

**Example Output:**
```json
[
  {
    "IP": "10.33.30.11",
    "DriveType": "OptidriveE3",
    "Group": 1,
    "FanNumber": 1,
    "FanDesc": "1x 1800RPM 29.5kCFM",
    "RpmHz": 30,
    "CfmRpm": 16.38888888,
    "Port": 502,
    "Unit": 1,
    "setSpeed": 45.0,
    "actualSpeed": 44.8,
    "rpmSpeed": 1344,
    "actualCfm": 22000,
    "current": 8.2,
    "status": "Running",
    "lastUpdated": 1718030000
    // ... other live fields ...
  },
  // ... more drives ...
]
```

Returns an array of objects, each containing both static config and live data for every drive.

### 🔗 `/api/control` (POST)

Remotely start, stop, set speed, or hold fans. Accepts a JSON payload:

```json
{
  "drives": ["10.33.30.11", "10.33.30.12"],
  "action": "SetSpeed", // One of: "Start", "Stop", "Fanhold", "Freespin", "SetSpeed"
  "speed": 45.0          // (Hz) Only required for SetSpeed
}
```

- 🖥️ `drives`: List of VFD IPs to control
- 🏷️ `action`: Control action (see below)
- ⚡ `speed`: (Optional) Frequency in Hz for `SetSpeed`

**Actions:**
- ▶️ `Start`: Start the selected drives
- ⏹️ `Stop`: Stop the selected drives
- 💤 `Fanhold`: Set speed to 0 Hz but keep drive enabled
- 🌀 `Freespin`: Alias for Stop (let fan coast)
- 🎚️ `SetSpeed`: Set the speed (Hz) and start the drive

```bash
curl -X POST http://10.33.10.53/api/control \
  -H 'Content-Type: application/json' \
  -d '{"drives": ["10.33.30.11"], "action": "SetSpeed", "speed": 45.0}'
```

> ✅ **Success:** `200 OK` with message `Control action processed successfully` or error details

### 📜 `/api/control-events` (GET)

Fetch a list of recent control events (for audit/logging). 🕒

```bash
curl http://10.33.10.53/api/control-events
```

```json
[
  {
    "timestamp": "2024-06-10T12:34:56Z",
    "action": "SetSpeed",
    "speed": 45.0,
    "drives": [
      { "ip": "10.33.30.11", "success": true },
      { "ip": "10.33.30.12", "success": false, "error": "Tripped" }
    ]
  }
]
```

### 🔌 `/api/vfdconnect` (POST)

Connect, disconnect, or toggle VFD connectivity. Also supports bulk operations and generates a single aggregated control event per request.

Request options:
- Single toggle:
```json
{ "ip": "10.33.30.11" }
```
- Bulk connect or disconnect:
```json
{ "ips": ["10.33.30.11", "10.33.30.12"], "action": "connect" }
```
```json
{ "ips": ["10.33.30.11", "10.33.30.12"], "action": "disconnect" }
```
- Bulk toggle (omit action):
```json
{ "ips": ["10.33.30.11", "10.33.30.12"] }
```

Examples:
```bash
# Single toggle
curl -X POST http://10.33.10.53/api/vfdconnect \
  -H 'Content-Type: application/json' \
  -d '{"ip": "10.33.30.11"}'

# Bulk disconnect
curl -X POST http://10.33.10.53/api/vfdconnect \
  -H 'Content-Type: application/json' \
  -d '{"ips": ["10.33.30.11","10.33.30.12"], "action": "disconnect"}'

# Bulk connect
curl -X POST http://10.33.10.53/api/vfdconnect \
  -H 'Content-Type: application/json' \
  -d '{"ips": ["10.33.30.11","10.33.30.12"], "action": "connect"}'
```

### 📊 `/api/status` (GET)

Get system status information including loading state, connection status, and data collection metrics. 📈

```bash
curl http://10.33.10.53/api/status
```

**Example Output:**
```json
{
  "loading": false,
  "ready": true,
  "initialConnectionsDone": true,
  "totalVFDs": 24,
  "connectedVFDs": 22,
  "healthyVFDs": 21,
  "lastUpdateTime": "2025-08-06T12:34:56Z",
  "dataCollectionAge": "5.2s"
}
```

**Response Fields:**
- `loading`: True if system is still establishing initial connections
- `ready`: True if system has completed initial setup and is operational
- `initialConnectionsDone`: True after first connection attempt to all VFDs
- `totalVFDs`: Total number of configured VFDs
- `connectedVFDs`: Number of successfully connected VFDs
- `healthyVFDs`: Number of healthy/responsive VFDs
- `lastUpdateTime`: Timestamp of last data collection cycle
- `dataCollectionAge`: How long ago data was last collected

This endpoint is particularly useful for external monitoring systems and the curtail dashboard to determine if the VFD server is still initializing or ready for operations.

### 🔻 `/api/curtail` (POST)

Curtail and resume VFD operations. Curtailment saves the current state of all or selected drives, stops them, and allows resuming to their previous state later. 🛑

**Curtail Request:**
```json
{
  "action": "curtail",
  "groups": ["1", "B1-A"]  // Empty array or omit = curtail all drives
}
```

**Resume Request:**
```json
{
  "action": "resume"
}
```

**Example - Curtail specific groups:**
```bash
curl -X POST http://10.33.10.53/api/curtail \
  -H 'Content-Type: application/json' \
  -d '{"action": "curtail", "groups": ["1", "B1-A"]}'
```

**Example - Curtail ALL drives:**
```bash
curl -X POST http://10.33.10.53/api/curtail \
  -H 'Content-Type: application/json' \
  -d '{"action": "curtail", "groups": []}'
```

**Example - Resume from curtailment:**
```bash
curl -X POST http://10.33.10.53/api/curtail \
  -H 'Content-Type: application/json' \
  -d '{"action": "resume"}'
```

**Response:**
```json
{
  "success": true,
  "message": "Curtailment applied to 15 drives",
  "driveCount": 15,
  "groups": ["1", "B1-A"],
  "timestamp": "2025-11-03T14:30:00Z"
}
```

**How it works:**
- **Curtail**: Saves current setpoint and status for each drive, stops all affected drives, and stores state in `/etc/vfd/curtailment_state.json`
- **Resume**: Loads saved state, restores each drive to its previous speed and running state, then clears the state file
- **Groups**: If no groups specified (empty array), curtails ALL configured drives
- **Persistence**: State survives server restarts - curtailed drives remain stopped until manually resumed

> 💡 **Use case**: Demand response, load shedding, emergency shutdown with automatic state restoration

## 🏗️ Building the Server

### 🧰 Prerequisites

- 🐧 Ubuntu 24.04
- 🦦 Go 1.23.2 (do not use system default if older)

### 📥 Install Go 1.23.2

```bash
sudo apt update
sudo apt install wget tar
wget https://go.dev/dl/go1.23.2.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.23.2.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
go version  # Should show go1.23.2
```

### 🏗️ Build the Server

```bash
cd vfdserver
go build -o vfdserver vfdserver.go
```

### 🧪 Run the Tests

```bash
go test ./...
```

Covers the frequency conversion formulas for every supported drive type, all three status-decoding paths (integer-based, bit-based, GS4-4020 EnabledStatus), and curtailment group filtering.

### 📂 Place Config Files

> 📂 **Production files location required:**
> - `/etc/vfd/config.json`
> - `/etc/vfd/drive_profiles.json`
> - `/etc/vfd/index.html`
> - `/usr/bin/vfdserver`

```bash
sudo mkdir -p /etc/vfd
sudo cp config.json /etc/vfd/
sudo cp drive_profiles.json /etc/vfd/
sudo cp index.html /etc/vfd/
sudo mv vfdserver /usr/bin/
```

---

## 🛡️ Running with Supervisord

### 🛠️ Install supervisord

```bash
sudo apt update
sudo apt install supervisor
```

### ⚙️ Example supervisord config

Create `/etc/supervisor/conf.d/vfdserver.conf`:

```ini
[program:vfdserver]
command=/path/to/vfdserver/vfdserver
directory=/path/to/vfdserver
autostart=true
autorestart=true
stderr_logfile=/var/log/vfdserver.err.log
stdout_logfile=/var/log/vfdserver.out.log
user=youruser
environment=PATH="/usr/local/go/bin:%(ENV_PATH)s"
```

> ⚠️ **Replace `/path/to/vfdserver` and `youruser` as appropriate.**

Reload and start:

```bash
sudo supervisorctl reread
sudo supervisorctl update
sudo supervisorctl start vfdserver
```

---

## 📈 Prometheus Integration

- 📊 Metrics are available at `http://<BindIP>:80/metrics` for Prometheus scraping.
- 📝 Example Prometheus scrape config:

```yaml
scrape_configs:
  - job_name: 'vfdserver'
    static_configs:
      - targets: ['10.33.10.53:80']
```

**Available Metrics:**
- `up`: VFD connection status (1=connected, 0=disconnected) - Standard Prometheus convention
- `vfd_status`: VFD operational status (1=running, 0=stopped) 
- `vfd_speed_hz`: Current VFD speed in Hertz
- `vfd_speed_rpm`: Current VFD speed in RPM
- `vfd_speed_percent`: Current VFD speed as percentage
- `vfd_amperage`: Current VFD amperage usage
- `vfd_cfm`: Current fan CFM (Cubic Feet per Minute)

---

## 🔒 Security

- 🚫 **No authentication is built-in.**
- 🛡️ **Strongly recommended:** Run behind a reverse proxy (e.g., nginx) for HTTPS and access control.

```nginx
server {
  listen 443 ssl;
  server_name vfd.example.com;
  ssl_certificate /etc/letsencrypt/live/vfd.example.com/fullchain.pem;
  ssl_certificate_key /etc/letsencrypt/live/vfd.example.com/privkey.pem;
  location / {
    proxy_pass http://10.33.10.53:80;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
  }
}
```

---

## 🛠️ Troubleshooting

> ❗ **Server fails to start:**
> - 📝 Check `/var/log/vfdserver.err.log` (if using supervisord)
> - 📂 Ensure `/etc/vfd/config.json` and `/etc/vfd/drive_profiles.json` exist and are valid JSON
> - 🦦 Ensure Go version is 1.23.2 or newer
>
> ❗ **Web UI not updating:**
> - 🖥️ Check browser console for WebSocket errors
> - 🌐 Ensure server is running and accessible on the network
>
> ❗ **Drives not responding:**
> - 🌐 Check network connectivity to VFDs
> - ⚙️ Verify Modbus settings in config.json
> - 🧩 Check drive_profiles.json for correct register mappings

---

## 🧾 Recent Changes

**2026-07 — UI redesign on the shared PDU/VFD theme kit (v3.8.1)**
- Rebuilt the web UI on the same design system as pduserver so both control panels look like one product: identical design tokens, Inter/IBM Plex Mono/Doto type, group cards with animated collapse, labeled k/v readout cells, status chips, border-left activity feed, toasts, and the fixed API pill
- Signature "SITE AIRFLOW" LCD meter in the masthead (glowing dot-matrix total CFM with fans-on and average-speed side stats), the VFD counterpart to pduserver's SITE LOAD meter
- Flicker-free live updates: drive rows update in place instead of rebuilding the DOM every second; the events feed only re-renders on change
- WebSocket auto-reconnect with a LIVE/STALE indicator and live clock in the masthead (a dropped socket previously froze the page until reload)
- Dark theme is now the default (matching pduserver); FontAwesome dependency replaced with an inline SVG fan that still shows spin direction
- Responsive mobile layout and reduced-motion support

**2026-07 — Concurrency fixes, hardening, and tests (v3.8.1)**
- Fixed a data race on the disabled-drives map that could crash the whole server (concurrent map read/write panic)
- Guaranteed exactly one connection-manager goroutine per drive; disable/enable cycles previously leaked duplicate managers fighting over the same drive (a problem for CFW500, which accepts only one TCP connection)
- Fixed a file-descriptor leak: dead TCP handlers are now closed before reconnecting
- Connection health flags are now atomic; poll cycles are serialized so stale snapshots can never overwrite fresh data
- Frequency-conversion expressions are parsed once at startup instead of on every register read (verified bit-identical output)
- Added a 10s HTTP read-header timeout to drop half-open connections (WebSockets unaffected)
- Added unit tests (`vfdserver_test.go`) and consolidated ~200 lines of duplicated code into shared helpers — no changes to Modbus behavior, timings, or API responses

**2026-02 — CFW500 speed control fixes (v3.8.1)**
- CFW500 speed control now uses P0222=12 (Ethernet mode) for SoftPLC speed pass-through via P1012; the server verifies and restores P0222 on every connect
- Fixed connection health recovery after network interruptions

---

## 📄 License

MIT License

Copyright (c) 2025 Louis Valois

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

---

## 🙏 Credits

- Developed by Louis Valois
- 🔗 Uses [grid-x/modbus](https://github.com/grid-x/modbus), [gorilla/websocket](https://github.com/gorilla/websocket), and [prometheus/client_golang](https://github.com/prometheus/client_golang) 
