// VFD Control Server v3.7.1 by Louis Valois - built for AAIMDC using OptidriveP2 and E3 Drives.
// This version includes many new improvements such as persistent VFD connections (that we can toggle on/off on a per-vfd basis)
// Faster websocket data refresh with back-end contiuously polling VFDs and serving front-ends from a global cache.
// Modular VFD control and status functions based on drive profiles.
// Added /devices json endpoint to retreive active devices with stats. Added API doc on the front-end.
// Much more, including major UI revamp and improvements.
// Added support for Automation Direct GS4-4020 and WEG CFW500 drives with some improvements as well.
// v3.7.1: Fixed OptidriveP2/E3 compatibility with INPUT register addressing and frequency calculations.

// =====================
// Imports
// =====================
package main

import (
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "os"
    "context"
    "time"
    "sync"
    "math"
    "github.com/grid-x/modbus"
    "github.com/gorilla/websocket"
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "strings"
)

// =====================
// Version
// =====================
const Version = "3.7.1"

// =====================
// Type Definitions
// =====================

type AppConfig struct {
    SiteName   string        `json:"SiteName"`
    BindIP     string        `json:"BindIP"`
    BindPort   string        `json:"BindPort"`
    NoFanHold  bool          `json:"NoFanHold"`
    GroupLabel string        `json:"GroupLabel"`
    VFDs       []DriveConfig `json:"VFDs"`
}

type DriveConfig struct {
    IP           string  `json:"IP"`
    Port         int     `json:"Port"`
    Unit         int     `json:"Unit"`
    DefaultSpeed int     `json:"DefaultSpeed"`
    Group        string  `json:"Group"`
    FanNumber    int     `json:"FanNumber"`
    FanDesc      string  `json:"FanDesc"`
    RpmToHz      float64 `json:"RpmHz"`
    CfmRpm       float64 `json:"CfmRpm"`
    DriveType    string  `json:"DriveType"`
    LastPull     int64   `json:"-"`
}

type VFDConfig map[string][]DriveConfig

type VFDConnection struct {
    handler *modbus.TCPClientHandler
    client  modbus.Client
    mu      sync.Mutex
    ip      string
    port    int
    unit    byte
    healthy bool
    retryCount int
    lastFail time.Time
}

type ControlEvent struct {
    Timestamp time.Time `json:"timestamp"`
    Action    string    `json:"action"`
    Speed     float64   `json:"speed"`
    Drives    []DriveEventInfo `json:"drives"`
}

type DriveEventInfo struct {
    IP      string `json:"ip"`
    Success bool   `json:"success"`
    Error   string `json:"error,omitempty"`
}

type CurtailmentState struct {
    Timestamp time.Time              `json:"timestamp"`
    Groups    []string               `json:"groups"`
    Drives    []CurtailedDriveState  `json:"drives"`
}

type CurtailedDriveState struct {
    IP       string  `json:"ip"`
    Group    string  `json:"group"`
    SetSpeed float64 `json:"setSpeed"`
    Status   string  `json:"status"`
}

const curtailmentStateFile = "/etc/vfd/curtailment_state.json"

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

// DriveTypeProfile holds register settings for each drive type
type DriveTypeProfile struct {
    RegisterType    string         `json:"RegisterType"`
    Setpoint        []int          `json:"Setpoint"`
    Control         int            `json:"Control"`
    SpeedPresetMultiplier int      `json:"SpeedPresetMultiplier"`
    OutputFrequency int            `json:"OutputFrequency"`
    OutputCurrent   int            `json:"OutputCurrent"`
    Status          int            `json:"Status"`
    StatusBits      map[string]int `json:"StatusBits"`
    StartValue      int            `json:"StartValue"`
    StopValue       int            `json:"StopValue"`
    UnTripRegister  int            `json:"UnTripRegister"`
    UnTripValue     int            `json:"UnTripValue"`
    OutFreqCalc     string         `json:"OutFreqCalc"`
    SetFreqCalc     string         `json:"SetFreqCalc"`
    OutCurrentCalc  string         `json:"OutCurrentCalc"`
    SignedOutputFreq bool          `json:"SignedOutputFreq"`
    MinHz           int            `json:"MinHz"`
    EnabledStatus   int            `json:"EnabledStatus"`
}

// =====================
// System Status Types
// =====================

type SystemStatus struct {
    Loading              bool          `json:"loading"`              // True if system is still loading/connecting to VFDs
    Ready                bool          `json:"ready"`                // True if system has completed initial connections
    InitialConnectionsDone bool        `json:"initialConnectionsDone"` // True after first connection attempt to all VFDs
    TotalVFDs            int           `json:"totalVFDs"`            // Total number of configured VFDs
    ConnectedVFDs        int           `json:"connectedVFDs"`        // Number of successfully connected VFDs
    HealthyVFDs          int           `json:"healthyVFDs"`          // Number of healthy/responsive VFDs
    LastUpdateTime       time.Time     `json:"lastUpdateTime"`       // When we last updated VFD data
    DataCollectionAge    time.Duration `json:"dataCollectionAge"`    // How long ago we last collected data
}

// =====================
// Global Variables
// =====================
var appConfig AppConfig
var vfdConfig [][]DriveConfig
var vfdData      []map[string]interface{}
var vfdDataMutex sync.RWMutex
var vfdConnections map[string]*VFDConnection
var vfdConnectionsMu sync.RWMutex
var systemStatus SystemStatus
var statusMutex sync.RWMutex
var controlEvents []ControlEvent
var driveTypeProfiles map[string]DriveTypeProfile
var disabledDrives = make(map[string]bool)
var eventsMutex sync.RWMutex

const (
    controlEventsFilePath = "/etc/vfd/control_events.json"
    controlEventsRetention = 100
)

// Persist and restore control events for retention across restarts
func loadControlEvents(filePath string) {
    file, err := os.Open(filePath)
    if err != nil {
        return
    }
    defer file.Close()

    var loaded []ControlEvent
    decoder := json.NewDecoder(file)
    if err := decoder.Decode(&loaded); err != nil {
        log.Printf("Failed to decode control events from %s: %v", filePath, err)
        return
    }

    if len(loaded) > controlEventsRetention {
        loaded = loaded[len(loaded)-controlEventsRetention:]
    }

    eventsMutex.Lock()
    controlEvents = loaded
    eventsMutex.Unlock()
}

func saveControlEvents(filePath string) {
    eventsMutex.RLock()
    snapshot := make([]ControlEvent, len(controlEvents))
    copy(snapshot, controlEvents)
    eventsMutex.RUnlock()

    file, err := os.Create(filePath)
    if err != nil {
        log.Printf("Failed to create %s: %v", filePath, err)
        return
    }
    defer file.Close()

    encoder := json.NewEncoder(file)
    encoder.SetIndent("", "  ")
    if err := encoder.Encode(snapshot); err != nil {
        log.Printf("Failed to encode control events to %s: %v", filePath, err)
    }
}

// =====================
// Utility/Helper Functions
// =====================
func boolToFloat(b bool) float64 {
    if b {
        return 1
    }
    return 0
}

func safeFloat(v interface{}) float64 {
    if f, ok := v.(float64); ok {
        return f
    }
    return 0.0
}

func safeInt(v interface{}) int {
    if i, ok := v.(int); ok {
        return i
    }
    if f, ok := v.(float64); ok {
        return int(f)
    }
    return 0
}

// Helper to find drive type for a given IP
func findDriveType(ip string) (string, bool) {
    for _, drives := range vfdConfig {
        for _, d := range drives {
            if d.IP == ip {
                return d.DriveType, true
            }
        }
    }
    return "", false
}

// Update statusToString to handle both bit-based and integer-based status
func statusToString(status int, statusBits map[string]int, enabledStatus int) string {
    // If StatusBits is not defined or empty, use integer-based status
    if statusBits == nil || len(statusBits) == 0 {
        // Integer-based status: 0 = no fault, > 0 = fault (Inhibited)
        if status == 0 {
            return "Running"
        } else {
            return "Inhibited"
        }
    }
    
    // Special handling for GS44020 (enabled status from register 8449, inhibited from 8448)
    if enabledStatus > 0 {
        driveEnabled := (enabledStatus & (1 << 0)) != 0  // Bit 0 from register 8449
        driveInhibited := (status & (1 << 3)) != 0       // Bit 3 from register 8448
        
        if driveInhibited {
            return "NotReady"
        }
        if driveEnabled {
            return "Running"
        } else {
            return "Stopped"
        }
    }
    
    // Bit-based status (legacy behavior for other drives)
    driveEnabled := (status & (1 << statusBits["Enabled"])) != 0
    driveTripped := (status & (1 << statusBits["Tripped"])) != 0
    driveInhibited := false
    if bit, ok := statusBits["Inhibited"]; ok {
        driveInhibited = (status & (1 << bit)) != 0
    }
    if driveTripped {
        return "Tripped"
    }
    if driveInhibited {
        return "NotReady"
    }
    if driveEnabled {
        return "Running"
    }    
    if !driveEnabled {
        return "Stopped"
    }
    return "Unknown"
}

// Helper to read a single register as signed integer and return its value as float64
func readSignedRegister(ctx context.Context, client modbus.Client, reg int) (float64, error) {
    res, err := client.ReadHoldingRegisters(ctx, uint16(reg), 1)
    if err != nil {
        return 0, fmt.Errorf("read error for reg %d: %w", reg, err)
    }
    if len(res) < 2 {
        return 0, fmt.Errorf("insufficient data for reg %d: got %d bytes", reg, len(res))
    }
    // Convert 2 bytes to signed 16-bit value
    value := int16(res[0])<<8 | int16(res[1])
    return float64(value), nil
}

// =====================
// Drive Profile & Connection Management
// =====================
func loadDriveTypeProfiles(path string) error {
    file, err := os.Open(path)
    if err != nil {
        return err
    }
    defer file.Close()
    decoder := json.NewDecoder(file)
    return decoder.Decode(&driveTypeProfiles)
}

func saveDisabledDrives() {
    file, err := os.Create("/etc/vfd/disabled_drives.json")
    if err == nil {
        defer file.Close()
        encoder := json.NewEncoder(file)
        encoder.Encode(disabledDrives)
    }
}

func loadDisabledDrives() {
    file, err := os.Open("/etc/vfd/disabled_drives.json")
    if err == nil {
        defer file.Close()
        decoder := json.NewDecoder(file)
        decoder.Decode(&disabledDrives)
    }
}

func manageVFDConnection(vfd *DriveConfig) {
    ip := vfd.IP
    port := vfd.Port
    unit := byte(vfd.Unit)
    var conn *VFDConnection
    var err error
    wasUnavailable := false

    for {
        // 1. If disabled, sleep and skip connection attempts
        if disabledDrives[ip] {
            time.Sleep(10 * time.Second)
            continue
        }

        // 2. Try to connect up to 3 times
        var lastErr error
        for i := 0; i < 3; i++ {
            conn, err = connectVFD(ip, port, unit)
            if err == nil {
                vfdConnectionsMu.Lock()
                vfdConnections[ip] = conn
                vfdConnectionsMu.Unlock()
                if wasUnavailable {
                    log.Printf("VFD %s is now AVAILABLE (reconnected)", ip)
                    wasUnavailable = false
                }
                goto Connected
            }
            lastErr = err
            if i == 2 {
                log.Printf("VFD %s: 3 connection attempts failed. Last error: %v. Retrying in 5 minutes.", ip, lastErr)
            }
            time.Sleep(5 * time.Second)
        }
        // 3. After 3 failures, mark as unavailable and backoff
        vfdConnectionsMu.Lock()
        if conn != nil {
            conn.healthy = false
        }
        vfdConnectionsMu.Unlock()
        if !wasUnavailable {
            wasUnavailable = true
        }
        time.Sleep(5 * time.Minute)
        continue

    Connected:
        // 4. Health check loop
        for {
            if disabledDrives[ip] {
                // If disabled while connected, close and break to outer loop
                conn.mu.Lock()
                conn.handler.Close()
                conn.healthy = false
                conn.mu.Unlock()
                break
            }
            time.Sleep(5 * time.Second)
            conn.mu.Lock()
            _, err := conn.client.ReadHoldingRegisters(context.Background(), 0, 1)
            conn.mu.Unlock()
            if err != nil {
                log.Printf("Lost connection to %s: %v", ip, err)
                conn.healthy = false
                break // Go back to outer loop to retry connection
            }
        }
    }
}

func connectVFD(ip string, port int, unit byte) (*VFDConnection, error) {
    handler := modbus.NewTCPClientHandler(fmt.Sprintf("%s:%d", ip, port))
    handler.Timeout = 2 * time.Second
    handler.SlaveID = unit
    err := handler.Connect(context.Background())
    if err != nil {
        return nil, err
    }
    client := modbus.NewClient(handler)

    // Try to read a known register to verify the drive is present
    _, err = client.ReadHoldingRegisters(context.Background(), 0, 1) // e.g., status register
    if err != nil {
        handler.Close()
        return nil, fmt.Errorf("Connection by MODBUS probe failed: %w", err)
    }

    return &VFDConnection{
        handler: handler,
        client:  client,
        ip:      ip,
        port:    port,
        unit:    unit,
        healthy: true,
    }, nil
}

// Helper to read a single register and return its value as float64
func readInputRegister(ctx context.Context, client modbus.Client, reg int) (float64, error) {
    res, err := client.ReadInputRegisters(ctx, uint16(reg), 1)
    if err != nil {
        return 0, fmt.Errorf("read error for reg %d: %w", reg, err)
    }
    if len(res) < 2 {
        return 0, fmt.Errorf("insufficient data for reg %d: got %d bytes", reg, len(res))
    }
    // Convert 2 bytes to 16-bit value
    value := int(res[0])<<8 | int(res[1])
    return float64(value), nil
}

// Helper to read a single register and return its value as float64
func readHoldingRegister(ctx context.Context, client modbus.Client, reg int) (float64, error) {
    res, err := client.ReadHoldingRegisters(ctx, uint16(reg), 1)
    if err != nil {
        return 0, fmt.Errorf("read error for reg %d: %w", reg, err)
    }
    if len(res) < 2 {
        return 0, fmt.Errorf("insufficient data for reg %d: got %d bytes", reg, len(res))
    }
    // Convert 2 bytes to 16-bit value
    value := int(res[0])<<8 | int(res[1])
    return float64(value), nil
}

// =====================
// Polling & Data Collection
// =====================
func initializeVfdData() {
    vfdDataMutex.Lock()
    defer vfdDataMutex.Unlock()

    vfdData = make([]map[string]interface{}, 0, len(vfdConfig)*5)
    for _, drives := range vfdConfig {
        for _, d := range drives {
            vfdData = append(vfdData, map[string]interface{}{
                "group":         d.Group,
                "fanNumber":     d.FanNumber,
                "fanDesc":       d.FanDesc,
                "ip":            d.IP,
                "rpmToHz":       d.RpmToHz,
                "cfmRpm":        d.CfmRpm,
                "setSpeed":      0.0,
                "actualSpeed":   0.0,
                "actualPercent": 0.0,
                "rpmSpeed":      0,
                "actualCfm":     0,
                "current":       0.0,
                "clockwise":     1,
                "status":        "Waiting",
                "lastUpdated":   time.Now().Unix(),
            })
        }
    }
}

func pollAllDrives() {
    sem := make(chan struct{}, 10) // max 10 concurrent polls
    var wg sync.WaitGroup
    mu := sync.Mutex{}

    vfdDataMutex.Lock()
    currentData := vfdData // snapshot current data to preserve
    vfdDataMutex.Unlock()

    ipIndex := make(map[string]int)
    for i, d := range currentData {
        ipIndex[d["ip"].(string)] = i
    }

    newData := make([]map[string]interface{}, len(currentData))
    for i, d := range currentData {
        newMap := make(map[string]interface{}, len(d))
        for k, v := range d {
            newMap[k] = v
        }
        newData[i] = newMap
    }

    for _, drives := range vfdConfig {
        for _, d := range drives {
            if disabledDrives[d.IP] {
                // Mark as disabled in newData
                mu.Lock()
                if idx, ok := ipIndex[d.IP]; ok {
                    updated := newData[idx]
                    updated["status"] = "Disabled"
                    updated["actualSpeed"] = 0.0
                    updated["actualPercent"] = 0.0
                    updated["rpmSpeed"] = 0
                    updated["actualCfm"] = 0
                    updated["current"] = 0.0
                    updated["setSpeed"] = 0.0
                    updated["clockwise"] = 1
                    updated["lastUpdated"] = time.Now().Unix()
                }
                mu.Unlock()
                continue
            }
            vfdConnectionsMu.RLock()
            conn, ok := vfdConnections[d.IP]
            healthy := ok && conn.healthy
            vfdConnectionsMu.RUnlock()
            if !healthy {
                // Mark as offline in newData
                mu.Lock()
                if idx, ok := ipIndex[d.IP]; ok {
                    updated := newData[idx]
                    updated["status"] = "Unavailable"
                    updated["actualSpeed"] = 0.0
                    updated["actualPercent"] = 0.0
                    updated["rpmSpeed"] = 0
                    updated["actualCfm"] = 0
                    updated["current"] = 0.0
                    updated["setSpeed"] = 0.0
                    updated["clockwise"] = 1
                    updated["lastUpdated"] = time.Now().Unix()
                }
                mu.Unlock()
                continue
            }
            wg.Add(1)
            go func(d DriveConfig) {
                defer wg.Done()
                sem <- struct{}{}
                defer func() { <-sem }()
                ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
                defer cancel()
                data, err := pollDrive(ctx, d)
                if err != nil {
                    fmt.Printf("Drive %s Polling failed: %v\n", d.IP, err)
                    return
                }
                mu.Lock()
                if idx, ok := ipIndex[d.IP]; ok {
                    updated := newData[idx]
                    for k, v := range data {
                        updated[k] = v
                    }
                    updated["lastUpdated"] = time.Now().Unix()
                }
                mu.Unlock()
            }(d)
        }
    }
    wg.Wait()
    vfdDataMutex.Lock()
    vfdData = newData
    vfdDataMutex.Unlock()
}

func pollDrive(ctx context.Context, d DriveConfig) (map[string]interface{}, error) {
    vfdConnectionsMu.RLock()
    conn, ok := vfdConnections[d.IP]
    vfdConnectionsMu.RUnlock()
    if !ok || !conn.healthy {
        return nil, fmt.Errorf("No available connection for  %s", d.IP)
    }

    // Look up drive profile
    profile, ok := driveTypeProfiles[d.DriveType]
    if !ok {
        return nil, fmt.Errorf("unknown drive type profile: %s", d.DriveType)
    }

    conn.mu.Lock()
    defer conn.mu.Unlock()

    // Determine which register type to use based on profile
    useInputRegisters := (profile.RegisterType == "input")
    var err error

    // Read each required register individually
    var statusRaw float64
    if useInputRegisters {
        statusRaw, err = readInputRegister(ctx, conn.client, profile.Status)
    } else {
        statusRaw, err = readHoldingRegister(ctx, conn.client, profile.Status)
    }
    if err != nil {
        conn.healthy = false
        return nil, err
    }

    // Read enabled status for GS44020 drives
    var enabledStatusRaw float64
    if profile.EnabledStatus > 0 {
        if useInputRegisters {
            enabledStatusRaw, err = readInputRegister(ctx, conn.client, profile.EnabledStatus)
        } else {
            enabledStatusRaw, err = readHoldingRegister(ctx, conn.client, profile.EnabledStatus)
        }
        if err != nil {
            conn.healthy = false
            return nil, err
        }
    }

    var setSpeedRaw float64
    if useInputRegisters {
        setSpeedRaw, err = readInputRegister(ctx, conn.client, profile.Setpoint[0])
    } else {
        setSpeedRaw, err = readHoldingRegister(ctx, conn.client, profile.Setpoint[0])
    }
    if err != nil {
        conn.healthy = false
        return nil, err
    }

    // Read output frequency as signed or unsigned based on profile setting
    var outputFreqRaw float64
    if profile.SignedOutputFreq {
        outputFreqRaw, err = readSignedRegister(ctx, conn.client, profile.OutputFrequency)
    } else {
        if useInputRegisters {
            outputFreqRaw, err = readInputRegister(ctx, conn.client, profile.OutputFrequency)
        } else {
            outputFreqRaw, err = readHoldingRegister(ctx, conn.client, profile.OutputFrequency)
        }
    }
    if err != nil {
        conn.healthy = false
        return nil, err
    }

    var outputCurrentRaw float64
    if useInputRegisters {
        outputCurrentRaw, err = readInputRegister(ctx, conn.client, profile.OutputCurrent)
    } else {
        outputCurrentRaw, err = readHoldingRegister(ctx, conn.client, profile.OutputCurrent)
    }
    if err != nil {
        conn.healthy = false
        return nil, err
    }

    // Detect rotation direction based on output frequency sign
    clockwise := 1
    if outputFreqRaw < 0 {
        clockwise = 0
        // Convert negative frequency to positive for calculations
        outputFreqRaw = outputFreqRaw * -1
    }
    
    status := int(statusRaw)
    enabledStatus := int(enabledStatusRaw)
    setSpeed := applyFreqCalc(setSpeedRaw, profile.OutFreqCalc)
    actualSpeed := applyFreqCalc(outputFreqRaw, profile.OutFreqCalc)
    current := applyFreqCalc(outputCurrentRaw, profile.OutCurrentCalc)
    rpm := int(actualSpeed * d.RpmToHz)
    cfm := int(math.Round(float64(rpm) * d.CfmRpm))

    return map[string]interface{}{
        "setSpeed":      math.Round(setSpeed*10) / 10,
        "actualSpeed":   math.Round(actualSpeed*10) / 10,
        "actualPercent": math.Round((actualSpeed/0.6)*10) / 10,
        "rpmSpeed":      rpm,
        "actualCfm":     cfm,
        "current":       math.Round(current*10) / 10,
        "status":        statusToString(status, profile.StatusBits, enabledStatus),
        "clockwise":     clockwise,
    }, nil
}

// Helper to apply OutFreqCalc expression to the raw frequency value
func applyFreqCalc(raw float64, expr string) float64 {
    //fmt.Printf("applyFreqCalc: raw=%f, expr='%s'\n", raw, expr)
    
    // Default: divide by 10 (legacy behavior if no expr)
    if expr == "" {
        result := raw / 10.0
        //fmt.Printf("  No expression, using default: %f / 10.0 = %f\n", raw, result)
        return result
    }
    
    // Trim whitespace
    expr = strings.TrimSpace(expr)
    //fmt.Printf("  Trimmed expr: '%s'\n", expr)
    
    var val1, val2 float64
    
    // Try to parse "/ X * Y" format (divide first, then multiply)
    n, _ := fmt.Sscanf(expr, "/ %f * %f", &val1, &val2)
    if n == 2 {
        result := raw / val1 * val2
        //fmt.Printf("  Parsed '/ %f * %f': %f / %f * %f = %f\n", val1, val2, raw, val1, val2, result)
        return result
    }
    
    // Try to parse "* X / Y" format (multiply first, then divide)
    n, _ = fmt.Sscanf(expr, "* %f / %f", &val1, &val2)
    if n == 2 {
        result := raw * val1 / val2
        //fmt.Printf("  Parsed '* %f / %f': %f * %f / %f = %f\n", val1, val2, raw, val1, val2, result)
        return result
    }
    
    // Try to parse "* X" format (just multiply)
    n, _ = fmt.Sscanf(expr, "* %f", &val1)
    if n == 1 {
        result := raw * val1
        //fmt.Printf("  Parsed '* %f': %f * %f = %f\n", val1, raw, val1, result)
        return result
    }
    
    // Try to parse "/ X" format (just divide)
    n, _ = fmt.Sscanf(expr, "/ %f", &val1)
    if n == 1 {
        result := raw / val1
        //fmt.Printf("  Parsed '/ %f': %f / %f = %f\n", val1, raw, val1, result)
        return result
    }
    
    // fallback: just return raw
    //fmt.Printf("  Could not parse expression, returning raw: %f\n", raw)
    return raw
}

// =====================
// Modbus Command Functions
// =====================
func fanStop(ip string) error {
    vfdConnectionsMu.RLock()
    conn, ok := vfdConnections[ip]
    vfdConnectionsMu.RUnlock()
    if !ok || !conn.healthy {
        return fmt.Errorf("No available connection for  %s", ip)
    }
    driveType, ok := findDriveType(ip)
    if !ok {
        return fmt.Errorf("No drive profile for %s", ip)
    }
    profile, ok := driveTypeProfiles[driveType]
    if !ok {
        return fmt.Errorf("No drive profile for %s", ip)
    }
    conn.mu.Lock()
    defer conn.mu.Unlock()
    _, err := conn.client.WriteSingleRegister(context.Background(), uint16(profile.Control), uint16(profile.StopValue))
    return err
}

func fanUnTrip(ip string) error {
    vfdConnectionsMu.RLock()
    conn, ok := vfdConnections[ip]
    vfdConnectionsMu.RUnlock()
    if !ok || !conn.healthy {
        return fmt.Errorf("No available connection for  %s", ip)
    }
    driveType, ok := findDriveType(ip)
    if !ok {
        return fmt.Errorf("No drive profile for %s", ip)
    }
    profile, ok := driveTypeProfiles[driveType]
    if !ok {
        return fmt.Errorf("No drive profile for %s", ip)
    }
    conn.mu.Lock()
    defer conn.mu.Unlock()
    _, err := conn.client.WriteSingleRegister(context.Background(), uint16(profile.UnTripRegister), uint16(profile.UnTripValue))
    return err
}

func fanStart(ip string) error {
    vfdConnectionsMu.RLock()
    conn, ok := vfdConnections[ip]
    vfdConnectionsMu.RUnlock()
    if !ok || !conn.healthy {
        return fmt.Errorf("No available connection for  %s", ip)
    }
    driveType, ok := findDriveType(ip)
    if !ok {
        return fmt.Errorf("No drive profile for %s", ip)
    }
    profile, ok := driveTypeProfiles[driveType]
    if !ok {
        return fmt.Errorf("No drive profile for %s", ip)
    }
    conn.mu.Lock()
    defer conn.mu.Unlock()
    _, err := conn.client.WriteSingleRegister(context.Background(), uint16(profile.Control), uint16(profile.StartValue))
    return err
}

func setFanSpeed(ip string, setspeed float64) error {
    vfdConnectionsMu.RLock()
    conn, ok := vfdConnections[ip]
    vfdConnectionsMu.RUnlock()
    if !ok || !conn.healthy {
        return fmt.Errorf("No available connection for  %s", ip)
    }
    driveType, ok := findDriveType(ip)
    if !ok {
        return fmt.Errorf("No drive profile for %s", ip)
    }
    profile, ok := driveTypeProfiles[driveType]
    if !ok {
        return fmt.Errorf("No drive profile for %s", ip)
    }
    conn.mu.Lock()
    defer conn.mu.Unlock()
    actualSpeedSet := applyFreqCalc(setspeed, profile.SetFreqCalc)
    _, err := conn.client.WriteSingleRegister(context.Background(), uint16(profile.Control), uint16(profile.StartValue))
    if err != nil {
        return err
    }
    if len(profile.Setpoint) > 0 {
        _, err = conn.client.WriteSingleRegister(context.Background(), uint16(profile.Setpoint[0]), uint16(int(actualSpeedSet)))
        if err != nil {
            return err
        }
    }
    if len(profile.Setpoint) > 1 {
        _, err = conn.client.WriteSingleRegister(context.Background(), uint16(profile.Setpoint[1]), uint16(int(actualSpeedSet*float64(profile.SpeedPresetMultiplier))))
        if err != nil {
            return err
        }
    }
    return nil
}

func fanHold(ip string) error {
    vfdConnectionsMu.RLock()
    conn, ok := vfdConnections[ip]
    vfdConnectionsMu.RUnlock()
    if !ok || !conn.healthy {
        return fmt.Errorf("No available connection for  %s", ip)
    }
    driveType, ok := findDriveType(ip)
    if !ok {
        return fmt.Errorf("No drive profile for %s", ip)
    }
    profile, ok := driveTypeProfiles[driveType]
    if !ok {
        return fmt.Errorf("No drive profile for %s", ip)
    }
    conn.mu.Lock()
    defer conn.mu.Unlock()
    _, err := conn.client.WriteSingleRegister(context.Background(), uint16(profile.Control), uint16(profile.StartValue))
    if err != nil {
        return err
    }
    if len(profile.Setpoint) > 0 {
        _, err = conn.client.WriteSingleRegister(context.Background(), uint16(profile.Setpoint[0]), 0)
        if err != nil {
            return err
        }
    }
    if len(profile.Setpoint) > 1 {
        _, err = conn.client.WriteSingleRegister(context.Background(), uint16(profile.Setpoint[1]), 0)
        if err != nil {
            return err
        }
    }
    return nil
}

// =====================
// Curtailment Functions
// =====================

// loadCurtailmentState loads the curtailment state from file
func loadCurtailmentState() (*CurtailmentState, error) {
    data, err := os.ReadFile(curtailmentStateFile)
    if err != nil {
        return nil, err
    }
    var state CurtailmentState
    err = json.Unmarshal(data, &state)
    if err != nil {
        return nil, err
    }
    return &state, nil
}

// saveCurtailmentState saves the curtailment state to file
func saveCurtailmentState(state *CurtailmentState) error {
    data, err := json.MarshalIndent(state, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(curtailmentStateFile, data, 0644)
}

// clearCurtailmentState removes the curtailment state file
func clearCurtailmentState() error {
    err := os.Remove(curtailmentStateFile)
    if err != nil && !os.IsNotExist(err) {
        return err
    }
    return nil
}

// getDrivesForGroups returns all drives matching the specified groups
// If groups is empty, returns all drives
func getDrivesForGroups(groups []string) []DriveConfig {
    var drives []DriveConfig
    if len(groups) == 0 {
        // Return all drives
        drives = appConfig.VFDs
    } else {
        // Return only drives in specified groups
        for _, drive := range appConfig.VFDs {
            for _, group := range groups {
                if drive.Group == group {
                    drives = append(drives, drive)
                    break
                }
            }
        }
    }
    return drives
}

// curtailDrives saves current state and stops selected drives
func curtailDrives(groups []string) error {
    drives := getDrivesForGroups(groups)
    if len(drives) == 0 {
        return fmt.Errorf("no drives found for the specified groups")
    }

    state := CurtailmentState{
        Timestamp: time.Now(),
        Groups:    groups,
        Drives:    make([]CurtailedDriveState, 0),
    }

    // Get current state from vfdData
    vfdDataMutex.RLock()
    for _, drive := range drives {
        for _, entry := range vfdData {
            if entry["ip"] == drive.IP {
                curtailedDrive := CurtailedDriveState{
                    IP:    drive.IP,
                    Group: drive.Group,
                }
                if setSpeed, ok := entry["setSpeed"].(float64); ok {
                    curtailedDrive.SetSpeed = setSpeed
                }
                if status, ok := entry["status"].(string); ok {
                    curtailedDrive.Status = status
                }
                state.Drives = append(state.Drives, curtailedDrive)
                break
            }
        }
    }
    vfdDataMutex.RUnlock()

    // Save state to file
    err := saveCurtailmentState(&state)
    if err != nil {
        return fmt.Errorf("failed to save curtailment state: %w", err)
    }

    log.Printf("[CURTAIL] Saving state for %d drives in groups: %v", len(state.Drives), groups)

    // Stop all affected drives
    var wg sync.WaitGroup
    for _, drive := range state.Drives {
        wg.Add(1)
        go func(ip string) {
            defer wg.Done()
            err := fanStop(ip)
            if err != nil {
                log.Printf("[CURTAIL] Warning: Failed to stop drive %s: %v", ip, err)
            }
        }(drive.IP)
    }
    wg.Wait()

    log.Printf("[CURTAIL] Curtailment complete, %d drives stopped, state saved", len(state.Drives))
    return nil
}

// resumeDrives restores drives to their previous state
func resumeDrives() error {
    state, err := loadCurtailmentState()
    if err != nil {
        if os.IsNotExist(err) {
            return fmt.Errorf("no curtailment state found to resume")
        }
        return fmt.Errorf("failed to load curtailment state: %w", err)
    }

    log.Printf("[RESUME] Loading curtailment state from %s", state.Timestamp.Format(time.RFC3339))
    log.Printf("[RESUME] Restoring %d drives to previous state", len(state.Drives))

    // Restore each drive
    var wg sync.WaitGroup
    for _, drive := range state.Drives {
        wg.Add(1)
        go func(d CurtailedDriveState) {
            defer wg.Done()
            if d.Status == "Running" || d.Status == "Enabled" {
                // Restore speed and start the drive
                err := setFanSpeed(d.IP, d.SetSpeed)
                if err != nil {
                    log.Printf("[RESUME] Warning: Failed to restore drive %s: %v", d.IP, err)
                    return
                }
                log.Printf("[RESUME] Restored drive %s to %.1f Hz", d.IP, d.SetSpeed)
            } else {
                log.Printf("[RESUME] Drive %s was stopped, leaving stopped", d.IP)
            }
        }(drive)
    }
    wg.Wait()

    // Clear the curtailment state file
    err = clearCurtailmentState()
    if err != nil {
        log.Printf("[RESUME] Warning: Failed to clear curtailment state file: %v", err)
    }

    log.Printf("[RESUME] Resume complete, state cleared")
    return nil
}

// =====================
// HTTP/WebSocket Handlers
// =====================
func handleLivePage(w http.ResponseWriter, r *http.Request) {
        http.ServeFile(w, r, "/etc/vfd/index.html")
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
    log.Printf("WebSocket connection attempt from %s", r.RemoteAddr)
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println("WebSocket upgrade error:", err)
        return
    }
    defer conn.Close()
    log.Printf("WebSocket connection established from %s", r.RemoteAddr)

    // Send initial data immediately
    vfdDataMutex.RLock()
    initialData := make([]map[string]interface{}, len(vfdData))
    copy(initialData, vfdData)
    vfdDataMutex.RUnlock()

    log.Printf("Sending initial data to WebSocket client, data length: %d", len(initialData))
    if err := conn.WriteJSON(initialData); err != nil {
        log.Println("WebSocket initial write error:", err)
        return
    }
    log.Println("Initial data sent successfully")

    // Then send updated data every second
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            vfdDataMutex.RLock()
            data := make([]map[string]interface{}, len(vfdData))
            copy(data, vfdData)
            vfdDataMutex.RUnlock()

            if err := conn.WriteJSON(data); err != nil {
                log.Println("WebSocket write error:", err)
                return
            }
        }
    }
}

func handleControlEvents(w http.ResponseWriter, r *http.Request) {
    eventsMutex.RLock()
    events := make([]map[string]interface{}, len(controlEvents))
    for i, event := range controlEvents {
        events[i] = map[string]interface{}{
            "timestamp": event.Timestamp.Format(time.RFC3339),
            "action":    event.Action,
            "speed":     event.Speed,
            "drives":    event.Drives,
        }
    }
    eventsMutex.RUnlock()
    json.NewEncoder(w).Encode(events)
}

func handleControl(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
                return
        }

        var controlData struct {
                Drives []string `json:"drives"`
                Action string   `json:"action"`
                Speed  float64  `json:"speed"`  
        }
        err := json.NewDecoder(r.Body).Decode(&controlData)
        if err != nil {
                http.Error(w, "Failed to parse request body: "+err.Error(), http.StatusBadRequest)
                return
        }

        // Validate action
        if controlData.Action != "Freespin" && controlData.Action != "Fanhold" && controlData.Action != "SetSpeed" && controlData.Action != "Start" && controlData.Action != "Stop" {
                http.Error(w, "Invalid action", http.StatusBadRequest)
                return
        }

        log.Printf("[INCOMING REQUEST] Control action: Action=%s, Speed=%.2f, Drives=%v\n", controlData.Action, controlData.Speed, controlData.Drives)

    event := ControlEvent{
        Timestamp: time.Now(),
        Action:    controlData.Action,
        Speed:     controlData.Speed,
        Drives:    make([]DriveEventInfo, 0),
    }

    var wg sync.WaitGroup
    var mu sync.Mutex
    
    for _, ip := range controlData.Drives {
        wg.Add(1)
        go func(ip string) {
            defer wg.Done()
            driveInfo := DriveEventInfo{IP: ip, Success: true}
            var err error

            // Check drive status in vfdData
            vfdDataMutex.RLock()
            var driveStatus string
            for _, entry := range vfdData {
                if entry["ip"] == ip {
                    driveStatus, _ = entry["status"].(string)
                    break
                }
            }
            vfdDataMutex.RUnlock()

            if driveStatus == "Unavailable" || driveStatus == "NotReady" {
                driveInfo.Success = false
                driveInfo.Error = fmt.Sprintf("%s", driveStatus)
                log.Printf("[CONTROL BLOCKED] IP: %s, Action: %s, State: %s", ip, controlData.Action, driveStatus)
            } else {
                switch controlData.Action {
                case "Start":
                    if driveStatus == "Tripped" {
                        err = fanUnTrip(ip)
                        if err == nil {
                            err = fanStart(ip)
                        }
                    } else {
                        err = fanStart(ip)
                    }
                case "Stop":
                    err = fanStop(ip)
                case "Fanhold":
                    err = fanHold(ip)
                case "Freespin":
                    err = fanStop(ip)
                case "SetSpeed":
                    if driveStatus == "Tripped" {
                        err = fanUnTrip(ip)
                        if err == nil {
                            err = fanStart(ip)
                        }
                    } else {
                        err = fanStart(ip)
                    }
                    if err == nil {
                        err = setFanSpeed(ip, controlData.Speed)
                    }
                }
                if err != nil {
                    driveInfo.Success = false
                    driveInfo.Error = err.Error()
                    log.Printf("[MODBUS ERROR] IP: %s, Action: %s, Error: %s", ip, controlData.Action, err.Error())
                }
            }
            mu.Lock()
            event.Drives = append(event.Drives, driveInfo)
            mu.Unlock()
        }(ip)
    }
    wg.Wait()

    // Log the event with retention and persist
    eventsMutex.Lock()
    controlEvents = append(controlEvents, event)
    if len(controlEvents) > controlEventsRetention {
        controlEvents = controlEvents[1:]
    }
    eventsMutex.Unlock()
    saveControlEvents(controlEventsFilePath)

    w.Write([]byte("Control action processed successfully"))
    go pollAllDrives()
}

func handleCurtail(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
        return
    }

    var curtailData struct {
        Action string   `json:"action"` // "curtail" or "resume"
        Groups []string `json:"groups"` // Empty means all drives
    }
    err := json.NewDecoder(r.Body).Decode(&curtailData)
    if err != nil {
        http.Error(w, "Failed to parse request body: "+err.Error(), http.StatusBadRequest)
        return
    }

    // Validate action
    if curtailData.Action != "curtail" && curtailData.Action != "resume" {
        http.Error(w, "Invalid action, must be 'curtail' or 'resume'", http.StatusBadRequest)
        return
    }

    log.Printf("[CURTAIL] Received %s request for groups: %v", curtailData.Action, curtailData.Groups)

    var response map[string]interface{}

    if curtailData.Action == "curtail" {
        err = curtailDrives(curtailData.Groups)
        if err != nil {
            log.Printf("[CURTAIL] Error: %v", err)
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }

        drives := getDrivesForGroups(curtailData.Groups)
        response = map[string]interface{}{
            "success":    true,
            "message":    fmt.Sprintf("Curtailment applied to %d drives", len(drives)),
            "driveCount": len(drives),
            "groups":     curtailData.Groups,
            "timestamp":  time.Now().Format(time.RFC3339),
        }

        // Log control event
        event := ControlEvent{
            Timestamp: time.Now(),
            Action:    "Curtail",
            Speed:     0,
            Drives:    make([]DriveEventInfo, 0),
        }
        for _, drive := range drives {
            event.Drives = append(event.Drives, DriveEventInfo{
                IP:      drive.IP,
                Success: true,
            })
        }
        eventsMutex.Lock()
        controlEvents = append(controlEvents, event)
        if len(controlEvents) > controlEventsRetention {
            controlEvents = controlEvents[1:]
        }
        eventsMutex.Unlock()
        saveControlEvents(controlEventsFilePath)

    } else { // resume
        // Load state BEFORE resuming (resume clears the file)
        state, err := loadCurtailmentState()
        if err != nil {
            log.Printf("[RESUME] Error loading state: %v", err)
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }

        driveCount := len(state.Drives)
        groups := state.Groups

        // Now resume the drives
        err = resumeDrives()
        if err != nil {
            log.Printf("[RESUME] Error: %v", err)
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }

        response = map[string]interface{}{
            "success":    true,
            "message":    fmt.Sprintf("Resumed %d drives from curtailment", driveCount),
            "driveCount": driveCount,
            "groups":     groups,
            "timestamp":  time.Now().Format(time.RFC3339),
        }

        // Log control event
        event := ControlEvent{
            Timestamp: time.Now(),
            Action:    "Resume",
            Speed:     0,
            Drives:    make([]DriveEventInfo, 0),
        }
        for _, drive := range state.Drives {
            event.Drives = append(event.Drives, DriveEventInfo{
                IP:      drive.IP,
                Success: true,
            })
        }
        eventsMutex.Lock()
        controlEvents = append(controlEvents, event)
        if len(controlEvents) > controlEventsRetention {
            controlEvents = controlEvents[1:]
        }
        eventsMutex.Unlock()
        saveControlEvents(controlEventsFilePath)
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
    go pollAllDrives()
}

func handleAppConfig(w http.ResponseWriter, r *http.Request) {
    json.NewEncoder(w).Encode(map[string]interface{}{
        "siteName":   appConfig.SiteName,
        "groupLabel": appConfig.GroupLabel,
        "bindIP": appConfig.BindIP,
        "bindPort": appConfig.BindPort,
        "noFanHold": appConfig.NoFanHold,
    })
}

// HTTP handler to toggle a drive's enabled/disabled state by IP
func handleVFDConnect(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
        return
    }
    var req struct {
        IP     string   `json:"ip"`
        IPs    []string `json:"ips"`
        Action string   `json:"action"` // "connect", "disconnect", or empty for toggle
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Failed to parse request body: "+err.Error(), http.StatusBadRequest)
        return
    }
    // Determine list of IPs to operate on (bulk or single)
    targets := make([]string, 0)
    if len(req.IPs) > 0 {
        targets = append(targets, req.IPs...)
    } else if req.IP != "" {
        targets = append(targets, req.IP)
    } else {
        http.Error(w, "Missing 'ip' or 'ips' in request", http.StatusBadRequest)
        return
    }

    // Normalize action for logging and behavior
    normalized := strings.ToLower(strings.TrimSpace(req.Action))
    var logAction string
    switch normalized {
    case "connect":
        logAction = "ConnectVFD"
    case "disconnect":
        logAction = "DisconnectVFD"
    default:
        logAction = "ToggleVFD"
    }

    // Execute
    drives := make([]DriveEventInfo, 0, len(targets))
    for _, ip := range targets {
        success := true
        // Apply requested state or toggle
        if normalized == "connect" {
            if disabledDrives[ip] {
                delete(disabledDrives, ip)
                // Start/restart the connection goroutine for this drive
                for i := range appConfig.VFDs {
                    if appConfig.VFDs[i].IP == ip {
                        go manageVFDConnection(&appConfig.VFDs[i])
                        break
                    }
                }
            }
        } else if normalized == "disconnect" {
            disabledDrives[ip] = true
        } else { // toggle
            if disabledDrives[ip] {
                delete(disabledDrives, ip)
                for i := range appConfig.VFDs {
                    if appConfig.VFDs[i].IP == ip {
                        go manageVFDConnection(&appConfig.VFDs[i])
                        break
                    }
                }
            } else {
                disabledDrives[ip] = true
            }
        }
        drives = append(drives, DriveEventInfo{IP: ip, Success: success})
    }

    // Persist disabled drives and schedule poll once
    saveDisabledDrives()
    go pollAllDrives()

    // Log a single aggregated event
    agg := ControlEvent{
        Timestamp: time.Now(),
        Action:    logAction,
        Drives:    drives,
    }
    eventsMutex.Lock()
    controlEvents = append(controlEvents, agg)
    if len(controlEvents) > controlEventsRetention {
        controlEvents = controlEvents[1:]
    }
    eventsMutex.Unlock()
    saveControlEvents(controlEventsFilePath)

    // Response
    if len(targets) == 1 {
        if logAction == "ConnectVFD" { w.Write([]byte("Drive enabled")) } else if logAction == "DisconnectVFD" { w.Write([]byte("Drive disabled")) } else { w.Write([]byte("Drive toggled")) }
    } else {
        w.Write([]byte(fmt.Sprintf("Bulk %s applied to %d drives", logAction, len(targets))))
    }
}

// =====================
// System Status API
// =====================
func handleSystemStatus(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    
    statusMutex.RLock()
    vfdConnectionsMu.RLock()
    vfdDataMutex.RLock()
    
    // Calculate current system status
    totalVFDs := len(appConfig.VFDs)
    connectedVFDs := 0
    healthyVFDs := 0
    
    for _, vfd := range appConfig.VFDs {
        if conn, exists := vfdConnections[vfd.IP]; exists {
            if conn.healthy {
                connectedVFDs++
                healthyVFDs++
            }
        }
    }
    
    // Update system status
    status := systemStatus
    status.TotalVFDs = totalVFDs
    status.ConnectedVFDs = connectedVFDs
    status.HealthyVFDs = healthyVFDs
    
	// Consider system ready if initial connections are done and we have some VFD data
	status.Ready = status.InitialConnectionsDone && len(vfdData) > 0
	status.Loading = !status.Ready
	
	// Calculate data collection age properly
	if !status.LastUpdateTime.IsZero() {
		status.DataCollectionAge = time.Since(status.LastUpdateTime)
	} else {
		status.DataCollectionAge = 0
	}
	
	vfdDataMutex.RUnlock()
	vfdConnectionsMu.RUnlock()
	statusMutex.RUnlock()
	
	json.NewEncoder(w).Encode(status)
}

// =====================
// Devices API
// =====================
func handleDevices(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    vfdDataMutex.RLock()
    defer vfdDataMutex.RUnlock()
    // vfdData is []map[string]interface{} with live data
    // appConfig.VFDs is []DriveConfig with static config
    // Merge config and live data by IP
    drives := make([]map[string]interface{}, 0, len(vfdData))
    for _, live := range vfdData {
        drive := make(map[string]interface{})
        ip, _ := live["ip"].(string)
        // Find config for this IP
        var config *DriveConfig
        for i := range appConfig.VFDs {
            if appConfig.VFDs[i].IP == ip {
                config = &appConfig.VFDs[i]
                break
            }
        }
        if config != nil {
            drive["DriveType"] = config.DriveType
        }
        // Add live data
        for k, v := range live {
            drive[k] = v
        }
        drives = append(drives, drive)
    }
    json.NewEncoder(w).Encode(drives)
}

// =====================
// Prometheus Metrics
// =====================
var (
    // Define metrics with VFD namespace
    vfdstatus = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Namespace: "vfd",
            Name:     "status",
            Help:     "VFD operational status (1=running, 0=stopped)",
        },
        []string{"ip", "group", "fan_number"},
    )

    vfdspeedhz = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Namespace: "vfd",
            Name:     "speed_hz",
            Help:     "Current VFD speed in Hertz",
        },
        []string{"ip", "group", "fan_number"},
    )

    vfdspeedrpm = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Namespace: "vfd",
            Name:     "speed_rpm",
            Help:     "Current VFD speed in RPM",
        },
        []string{"ip", "group", "fan_number"},
    )

    vfdspeedpercent = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Namespace: "vfd",
            Name:     "speed_percent",
            Help:     "Current VFD speed in Percent",
        },
        []string{"ip", "group", "fan_number"},
    )    

        vfdamperage = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Namespace: "vfd",
            Name:     "amperage",
            Help:     "Current VFD amperage usage",
        },
        []string{"ip", "group", "fan_number"},
    )

        vfdcfm = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Namespace: "vfd",
            Name:     "cfm",
            Help:     "Current Fan CFM",
        },
        []string{"ip", "group", "fan_number"},
    )

        vfdup = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "up",
            Help: "VFD connection status (1=connected, 0=disconnected)",
        },
        []string{"ip", "group", "fan_number"},
    )

)

func init() {
    // Register metrics
    prometheus.MustRegister(vfdstatus)
    prometheus.MustRegister(vfdspeedhz)
    prometheus.MustRegister(vfdspeedrpm)
    prometheus.MustRegister(vfdspeedpercent)
    prometheus.MustRegister(vfdamperage)
    prometheus.MustRegister(vfdcfm)
    prometheus.MustRegister(vfdup)
}

// updateMetrics uses cached vfdData for Prometheus metrics
func updateMetrics() {
    ticker := time.NewTicker(15 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        collectMetrics()
    }
}

func collectMetrics() {
    vfdDataMutex.RLock()
    defer vfdDataMutex.RUnlock()

    for _, drive := range vfdData {
        ip, _ := drive["ip"].(string)
        group := fmt.Sprintf("%v", drive["group"])
        fan := fmt.Sprintf("%v", drive["fanNumber"])

        labels := prometheus.Labels{
            "ip":         ip,
            "group":      group,
            "fan_number": fan,
        }

        status := 0.0
        if drive["status"] == "Running" {
            status = 1.0
        }

        // Set "up" metric based on VFD availability
        up := 0.0
        driveStatus := fmt.Sprintf("%v", drive["status"])
        if driveStatus != "Unavailable" && driveStatus != "Disabled" {
            up = 1.0
        }

        vfdstatus.With(labels).Set(status)
        vfdup.With(labels).Set(up)
        vfdspeedhz.With(labels).Set(safeFloat(drive["actualSpeed"]))
        vfdspeedrpm.With(labels).Set(float64(safeInt(drive["rpmSpeed"])))
        vfdspeedpercent.With(labels).Set(safeFloat(drive["actualPercent"]))
        vfdcfm.With(labels).Set(float64(safeInt(drive["actualCfm"])))
        vfdamperage.With(labels).Set(safeFloat(drive["current"]))
    }
}

// =====================
// Main Function
// =====================
func main() {
        var err error

        // Initialize system status
        statusMutex.Lock()
        systemStatus = SystemStatus{
            Loading:                true,
            Ready:                  false,
            InitialConnectionsDone: false,
        }
        statusMutex.Unlock()

        // Load drive type profiles
        driveTypeProfiles = make(map[string]DriveTypeProfile)
        if err := loadDriveTypeProfiles("/etc/vfd/drive_profiles.json"); err != nil {
            log.Fatalf("Failed to load drive type profiles: %v", err)
        }

        loadDisabledDrives()
        
        file, err := os.Open("/etc/vfd/config.json")
        if err != nil {
                log.Fatal(err)
        }
        defer file.Close()
        decoder := json.NewDecoder(file)
        if err := decoder.Decode(&appConfig); err != nil {
                log.Fatal(err)
        }

        vfdConfig = make([][]DriveConfig, len(appConfig.VFDs))
        for i, vfd := range appConfig.VFDs {
                vfdConfig[i] = []DriveConfig{vfd}
        }
        
        initializeVfdData()

        vfdConnections = make(map[string]*VFDConnection)
        // Load persisted control events from previous runs
        loadControlEvents(controlEventsFilePath)
        for _, drives := range vfdConfig {
            for _, d := range drives {
                go manageVFDConnection(&d)
            }
        }

        // Start polling VFDs every second in the background
        go func() {
            ticker := time.NewTicker(1 * time.Second)
            defer ticker.Stop()

            for range ticker.C {
                pollAllDrives()
                
                // Update data timestamp
                statusMutex.Lock()
                systemStatus.LastUpdateTime = time.Now()
                statusMutex.Unlock()
            }
        }()
        
        // Mark initial connections as done after a brief delay to allow connections to establish
        go func() {
            time.Sleep(10 * time.Second) // Give VFDs time to connect
            statusMutex.Lock()
            systemStatus.InitialConnectionsDone = true
            statusMutex.Unlock()
            log.Println("VFD initial connection phase completed")
        }()
        
        go updateMetrics()

        http.HandleFunc("/", handleLivePage)
        http.HandleFunc("/ws", handleWebSocket)
        http.HandleFunc("/api/control", handleControl)
        http.HandleFunc("/api/control-events", handleControlEvents)
        http.HandleFunc("/api/curtail", handleCurtail)
        http.HandleFunc("/api/app-config", handleAppConfig)
        http.HandleFunc("/api/vfdconnect", handleVFDConnect)
        http.HandleFunc("/api/devices", handleDevices)
        http.HandleFunc("/api/status", handleSystemStatus)
        http.Handle("/metrics", promhttp.Handler())

        log.Printf("VFD Control Server v%s by Louis Valois - for %s Site\nWeb server started on http://%s:%s", Version, appConfig.SiteName, appConfig.BindIP, appConfig.BindPort)
        log.Fatal(http.ListenAndServe(appConfig.BindIP + ":" + appConfig.BindPort, nil))
}
