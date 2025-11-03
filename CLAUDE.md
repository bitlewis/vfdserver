# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

VFD Control Server v3.7.1 is a Go-based web server for real-time control and monitoring of industrial Variable Frequency Drives (VFDs). It provides:
- Persistent TCP/Modbus connections to VFDs with automatic reconnection
- Real-time WebSocket-based web UI with live status updates
- REST API for remote control and monitoring
- Prometheus metrics endpoint for observability
- Support for multiple drive types via modular drive profiles

**Technology Stack:**
- Language: Go 1.23.2
- Key Dependencies:
  - `github.com/grid-x/modbus` - Modbus TCP communication
  - `github.com/gorilla/websocket` - WebSocket server for live UI
  - `github.com/prometheus/client_golang` - Prometheus metrics

## Building and Running

**Build the server:**
```bash
go build -o vfdserver vfdserver.go
```

**Run in development:**
```bash
# Ensure config files exist in /etc/vfd/
sudo ./vfdserver
```

**Production deployment:**
- Binary: `/usr/bin/vfdserver`
- Config files: `/etc/vfd/config.json`, `/etc/vfd/drive_profiles.json`, `/etc/vfd/index.html`
- Run via supervisord (see README.md for supervisor config)

## Architecture

### Core Components

**Single-File Architecture:**
The entire server is in `vfdserver.go` (~1434 lines), organized into logical sections:
1. Type Definitions (lines 35-126)
2. Global Variables (lines 128-146)
3. Utility/Helper Functions (lines 195-293)
4. Drive Profile & Connection Management (lines 295-500)
5. VFD Control Functions (lines 700-900)
6. HTTP Handlers (lines 900-1300)
7. Prometheus Metrics (lines 1300-1400)
8. Main function and initialization (lines 1400-1434)

### Configuration System

**Two JSON config files:**

1. `/etc/vfd/config.json` - Site and VFD configuration:
   - `SiteName`: Display name for the site
   - `BindIP`: IP address to bind web server (use "0.0.0.0" for all interfaces)
   - `BindPort`: Port to listen on (default: "80")
   - `GroupLabel`: Label for logical groups (e.g., "POD", "Zone")
   - `NoFanHold`: If true, "Fanhold" action is disabled in UI
   - `VFDs[]`: Array of VFD configurations with IP, Port, Unit, Group, FanNumber, FanDesc, RpmHz, CfmRpm, DriveType

2. `/etc/vfd/drive_profiles.json` - Drive type profiles:
   - Maps drive types (e.g., "OptidriveP2", "OptidriveE3", "CFW500", "GS44020") to register addresses
   - Each profile defines: Setpoint registers, Control register, Status register, Output frequency/current registers
   - Includes calculation expressions (e.g., "* 100", "/ 60 * 8192") for converting between drive units
   - StatusBits map defines which bits indicate Enabled, Tripped, Inhibited states

### Connection Management

**Persistent VFD Connections:**
- Each VFD has a dedicated goroutine (`manageVFDConnection`) that maintains a persistent TCP/Modbus connection
- Automatic reconnection with exponential backoff (max 5 minutes between retries)
- Health monitoring: connections marked healthy/unhealthy based on read success
- Connections can be toggled on/off via `/api/vfdconnect` endpoint
- Disabled drives are persisted to `/etc/vfd/disabled_drives.json` across restarts

**Data Polling:**
- `pollAllDrives()` continuously polls all connected VFDs every 2 seconds
- Results stored in global `vfdData` array (protected by `vfdDataMutex`)
- WebSocket clients receive live updates from this cached data
- System status tracks loading state, connection counts, and data freshness

### Drive Control Flow

**Control Actions:**
All control actions follow the same pattern:
1. Client sends request to `/api/control` with action and target drives
2. Server validates request and acquires VFD connection
3. Server reads drive profile to get register addresses and values
4. Server writes appropriate values to Modbus registers using drive profile
5. Server records control event with success/failure for each drive
6. Control events persisted to `/etc/vfd/control_events.json` (last 100 events)

**Supported Actions:**
- `Start`: Set Control register to StartValue (usually 1)
- `Stop`: Set Control register to StopValue (usually 0)
- `SetSpeed`: Set Setpoint register(s) to frequency in Hz, then start
- `Fanhold`: Set speed to 0 Hz but keep drive enabled
- `Freespin`: Alias for Stop (let fan coast)

**Drive-Specific Behavior:**
- OptidriveP2: Uses two setpoint registers [1, 207] and SpeedPresetMultiplier
- OptidriveE3: Simple single setpoint register [1]
- CFW500: Requires frequency conversion (/ 60 * 8192) and signed output frequency
- GS44020: Uses separate EnabledStatus register (8449) and Status register (8448)

### Status Interpretation

**Status Determination:**
The `statusToString()` function interprets drive status based on profile:
- **Bit-based status** (OptidriveP2, OptidriveE3, CFW500):
  - Checks StatusBits map for Enabled, Tripped, Inhibited bit positions
  - Returns: "Running", "Stopped", "Tripped", "NotReady", "Unknown"
- **GS44020 special case**:
  - Uses EnabledStatus register (bit 0 = enabled)
  - Uses Status register bit 3 for Inhibited
  - Different logic path in statusToString()

### API Endpoints

**Key endpoints:**
- `GET /` - Serves web UI (index.html)
- `GET /ws` - WebSocket for live updates
- `GET /api/devices` - Returns all VFDs with live data
- `POST /api/control` - Execute control actions (Start, Stop, SetSpeed, Fanhold, Freespin)
- `GET /api/control-events` - Fetch recent control event history
- `POST /api/vfdconnect` - Toggle VFD connections (single or bulk)
- `GET /api/status` - System status (loading state, connection counts)
- `GET /metrics` - Prometheus metrics

**Control Event Persistence:**
- Control events are saved to `/etc/vfd/control_events.json` after each action
- Loaded on startup to provide history across restarts
- Maximum 100 events retained (oldest are trimmed)

### Prometheus Metrics

**Exposed metrics:**
- `up{ip, fan_number, group, site}` - Connection status (1=connected, 0=disconnected)
- `vfd_status{...}` - Operational status (1=running, 0=stopped)
- `vfd_speed_hz{...}` - Current speed in Hertz
- `vfd_speed_rpm{...}` - Current speed in RPM
- `vfd_speed_percent{...}` - Speed as percentage
- `vfd_amperage{...}` - Current amperage
- `vfd_cfm{...}` - Calculated CFM (Cubic Feet per Minute)

All metrics include labels: `ip`, `fan_number`, `group`, `site`

## Common Development Patterns

**Adding a new drive type:**
1. Add entry to `drive_profiles.json` with register mappings
2. Test with a single VFD first
3. Pay attention to frequency calculations (SetFreqCalc, OutFreqCalc)
4. Handle signed vs unsigned output frequency (SignedOutputFreq)
5. Define StatusBits or use EnabledStatus for GS44020-style drives

**Modifying control logic:**
- Control functions: `fanStart()`, `fanStop()`, `setFanSpeed()`, `fanHold()`, `fanUnTrip()`
- All use `findDriveType()` to get drive profile, then read/write appropriate registers
- Always use context with timeout for Modbus operations
- Lock connections with `conn.mu.Lock()` during operations

**Working with VFD connections:**
- Use `vfdConnectionsMu` (RWMutex) to protect `vfdConnections` map
- Each connection has its own mutex (`conn.mu`) for Modbus operations
- Connection health tracked in `conn.healthy` field
- Check `disabledDrives` map before attempting operations

## Important Considerations

**Configuration file locations are hardcoded:**
- `/etc/vfd/config.json`
- `/etc/vfd/drive_profiles.json`
- `/etc/vfd/index.html`
- `/etc/vfd/control_events.json`
- `/etc/vfd/disabled_drives.json`

**Thread safety:**
- `vfdDataMutex` protects `vfdData` array
- `vfdConnectionsMu` protects `vfdConnections` map
- `eventsMutex` protects `controlEvents` array
- `statusMutex` protects `systemStatus` struct
- Each VFDConnection has its own mutex for Modbus operations

**Error handling:**
- Connection errors trigger reconnection logic in `manageVFDConnection()`
- Control operation errors are recorded in control events with error messages
- Polling errors mark connections as unhealthy but don't stop polling

**System initialization:**
- Server marks `systemStatus.Loading = true` on startup
- Initial connections established in first 10 seconds
- `systemStatus.InitialConnectionsDone = true` after 10 seconds
- Used by external systems to determine if server is ready

## Versioning

**Current Version:** 3.7.1

The version is defined as a constant in `vfdserver.go`:
```go
const Version = "3.7.1"
```

**Version History:**
- **v3.7.1** (2025-11-03): Fixed OptidriveP2/E3 compatibility
  - Added `RegisterType` field to drive profiles to distinguish between INPUT and HOLDING registers
  - Corrected INPUT register addresses (Status: 6→5, OutputFrequency: 7→6, OutputCurrent: 8→7)
  - Fixed frequency calculations for Optidrives (SetFreqCalc: "* 10", OutFreqCalc: "/ 10")
  - Added `bindPort` to `/api/app-config` endpoint response

- **v3.7** (Base version): Initial modular drive profile architecture
  - Support for OptidriveP2, OptidriveE3, CFW500, and GS44020
  - Persistent VFD connections with automatic reconnection
  - WebSocket-based real-time UI
  - Prometheus metrics integration

**Version Update Process:**
1. Update the `Version` constant in `vfdserver.go`
2. Update the version in the header comment (line 1)
3. Add changelog entry in the comment (line 8)
4. Update `README.md` title
5. Update `CLAUDE.md` version references
6. Rebuild and test

## Git & Version Control
- Add and commit automatically whenever an entire task is finished
- Use descriptive commit messages that capture the full scope of changes
