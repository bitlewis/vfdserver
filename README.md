<p align="center">
  <img src="https://img.shields.io/badge/Go-1.23.2-blue?logo=go" alt="Go version" />
  <img src="https://img.shields.io/badge/Platform-Ubuntu%2024.04-orange?logo=ubuntu" alt="Ubuntu" />
  <img src="https://img.shields.io/badge/Status-Production-green" alt="Production Status" />
  <img src="https://img.shields.io/badge/License-MIT-brightgreen" alt="License" />
</p>

<p align="center">
  <img src="https://user-images.githubusercontent.com/991078/273420123-2e7e7e7e-2e7e-4e7e-8e7e-2e7e7e7e7e7e.png" width="120" alt="VFD Logo" />
</p>

<p align="center">
  <b>Modern, real-time web-based control and monitoring for industrial Variable Frequency Drives (VFDs).</b><br>
  <i>Built for AAIMDC, supporting OptidriveP2 and OptidriveE3 drives.</i>
</p>

---

## 🚀 Features

- ⚡ **Persistent VFD Connections**  
  Each VFD is managed with a persistent TCP connection for fast, reliable control and status updates. Automatic reconnection and health monitoring.
- 🌐 **WebSocket-Powered UI**  
  Real-time updates and control via a modern, mobile-friendly web interface. No page reloads required; all data is live.
- 🌀 **Group and Fan Management**  
  Organize VFDs into logical groups (e.g., "pods"). Control individual fans or entire groups with one click.
- 🧩 **Drive Profiles**  
  Modular support for different drive types via `drive_profiles.json`. Easily extendable for new drive models.
- 📊 **Prometheus Metrics**  
  Exposes `/metrics` endpoint for Prometheus monitoring and alerting.
- 📝 **Control Events Log**  
  View recent control actions and their results in the UI.
- 🌙 **Dark Mode**  
  Toggleable dark/light mode for the web UI.
- 🔄 **Supervisord Integration**  
  Run the server as a managed service with automatic restarts.
- 🛠️ **Configurable via JSON**  
  All drives and profiles are configured via JSON files in `/etc/vfd`.
- 🛡️ **Security-Ready**  
  Designed to be run behind a reverse proxy for authentication and HTTPS.

---

## File Structure

- `vfdserver/vfdserver.go` — Main Go server source code.
- `vfdserver/config.json` — Example configuration file for VFDs (copy to `/etc/vfd/config.json`).
- `vfdserver/drive_profiles.json` — Example drive type profiles (copy to `/etc/vfd/drive_profiles.json`).
- `vfdserver/index.html` — Web UI served by the Go backend.

**Production config location:**
- `/etc/vfd/config.json`
- `/etc/vfd/drive_profiles.json`
- `/etc/vfd/index.html`
- `/usr/bin/vfdserver`

---

## Configuration

### 1. `/etc/vfd/config.json`

Defines the site, network, and all VFDs to be managed.

**Example:**
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
- `SiteName`: Displayed in the UI and logs.
- `BindIP`: IP address to bind the web server (use `0.0.0.0` for all interfaces).
- `GroupLabel`: Label for groups (e.g., "POD", "Zone").
- `VFDs`: List of VFDs, each with:
  - `IP`: VFD IP address
  - `Port`: Modbus TCP port (usually 502)
  - `Unit`: Modbus unit ID
  - `FanNumber`: Logical fan number in group
  - `FanDesc`: Description (shown in UI)
  - `Group`: Group number
  - `RpmHz`: Conversion factor for RPM/Hz
  - `CfmRpm`: Conversion factor for CFM/RPM
  - `DriveType`: Must match a key in `drive_profiles.json`

### 2. `/etc/vfd/drive_profiles.json`

Defines register mappings and control logic for each supported drive type.

**Example:**
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
- Each key is a drive type (must match `DriveType` in config.json)
- Register addresses and control values are specific to your hardware

---

## Web Interface

- **Live Dashboard:**
  - View all VFDs, grouped, with real-time status (speed, CFM, amps, etc.)
- **Control Panel:**
  - Set speed (Hz or %), start/stop/hold fans, select all, and group quick controls
- **Event Log:**
  - See recent control actions and their results
- **Dark Mode:**
  - Toggle with the button in the top right
- **Responsive:**
  - Works on desktop and mobile

---

## 🌍 Remote Control API

Remotely control the VFD server by sending JSON commands to its HTTP endpoints. All endpoints are accessible on `http://<BindIP>:80` (as set in your config).

### `/control` (POST)

Remotely start, stop, set speed, or hold fans. Accepts a JSON payload:

**Payload:**
```json
{
  "drives": ["10.33.30.11", "10.33.30.12"],
  "action": "SetSpeed", // One of: "Start", "Stop", "Fanhold", "Freespin", "SetSpeed"
  "speed": 45.0          // (Hz) Only required for SetSpeed
}
```
- `drives`: List of VFD IPs to control
- `action`: Control action (see below)
- `speed`: (Optional) Frequency in Hz for `SetSpeed`

**Actions:**
- `Start`: Start the selected drives
- `Stop`: Stop the selected drives
- `Fanhold`: Set speed to 0 Hz but keep drive enabled
- `Freespin`: Alias for Stop (let fan coast)
- `SetSpeed`: Set the speed (Hz) and start the drive

**Example (curl):**
```bash
curl -X POST http://10.33.10.53/control \
  -H 'Content-Type: application/json' \
  -d '{"drives": ["10.33.30.11"], "action": "SetSpeed", "speed": 45.0}'
```

**Response:**
- `200 OK` with message `Control action processed successfully` or error details

### `/control-events` (GET)

Fetch a list of recent control events (for audit/logging).

**Example:**
```bash
curl http://10.33.10.53/control-events
```

**Response:**
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

### `/app-config` (GET)

Fetch the current app/site configuration (site name, group label, etc).

**Example:**
```bash
curl http://10.33.10.53/app-config
```

### `/vfdconnect` (POST)

(Dis)connect a VFD by IP. Used by the UI to reconnect or disconnect a drive.

**Payload:**
```json
{ "ip": "10.33.30.11" }
```

**Example:**
```bash
curl -X POST http://10.33.10.53/vfdconnect \
  -H 'Content-Type: application/json' \
  -d '{"ip": "10.33.30.11"}'
```

### `/ws` (WebSocket)

Live updates for all VFDs. Used by the web UI, but can be consumed by custom dashboards.

---

## Building the Server

### Prerequisites

- Ubuntu 24.04
- Go 1.23.2 (do not use system default if older)

### Install Go 1.23.2

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

### Build the Server

```bash
cd vfdserver
go build -o vfdserver vfdserver.go
mv vfdserver /usr/bin/
```

### Place Config Files

```bash
sudo mkdir -p /etc/vfd
sudo cp config.json /etc/vfd/
sudo cp drive_profiles.json /etc/vfd/
sudo cp index.html /etc/vfd/
```

---

## Running with Supervisord

### Install supervisord

```bash
sudo apt update
sudo apt install supervisor
```

### Example supervisord config

Create `/etc/supervisor/conf.d/vfdserver.conf`:

```
[program:vfdserver]
command=/usr/bin/vfdserver
directory=/usr/bin/
autostart=true
autorestart=true
stderr_logfile=/var/log/vfdserver.err.log
stdout_logfile=/var/log/vfdserver.out.log
user=youruser
environment=PATH="/usr/local/go/bin:%(ENV_PATH)s"
```

**Replace `/path/to/vfdserver` and `youruser` as appropriate.**

Reload and start:

```bash
sudo supervisorctl reread
sudo supervisorctl update
sudo supervisorctl start vfdserver
```

---

## Prometheus Integration

- Metrics are available at `http://<BindIP>/metrics` for Prometheus scraping.
- Example Prometheus scrape config:
  ```yaml
  scrape_configs:
    - job_name: 'vfdserver'
      static_configs:
        - targets: ['10.33.10.53:80']
  ```

---

## Security

- **No authentication is built-in.**
- **Strongly recommended:** Run behind a reverse proxy (e.g., nginx) for HTTPS and access control.
- Example nginx snippet:
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

## Troubleshooting

- **Server fails to start:**
  - Check `/var/log/vfdserver.err.log` (if using supervisord)
  - Ensure `/etc/vfd/config.json` and `/etc/vfd/drive_profiles.json` exist and are valid JSON
  - Ensure Go version is 1.23.2 or newer
- **Web UI not updating:**
  - Check browser console for WebSocket errors
  - Ensure server is running and accessible on the network
- **Drives not responding:**
  - Check network connectivity to VFDs
  - Verify Modbus settings in config.json
  - Check drive_profiles.json for correct register mappings

---

## License

MIT

---

## Credits

- Developed by Louis Valois for AAIMDC
- Uses [grid-x/modbus](https://github.com/grid-x/modbus), [gorilla/websocket](https://github.com/gorilla/websocket), and [prometheus/client_golang](https://github.com/prometheus/client_golang) 
