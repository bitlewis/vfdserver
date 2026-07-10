package main

import (
    "testing"
)

// Expressions used by the real drive profiles, with exact expected conversions.
func TestApplyFreqCalc(t *testing.T) {
    cases := []struct {
        name string
        raw  float64
        expr string
        want float64
    }{
        {"empty defaults to /10", 500, "", 50},
        {"Optidrive set * 10", 50, "* 10", 500},
        {"Optidrive out / 10", 500, "/ 10", 50},
        {"CFW500 set / 60 * 8192", 30, "/ 60 * 8192", 4096},
        {"CFW500 set full speed", 60, "/ 60 * 8192", 8192},
        {"CFW500 out * 60 / 8192", 8192, "* 60 / 8192", 60},
        {"GS4 set * 100", 45, "* 100", 4500},
        {"GS4 out / 100", 4500, "/ 100", 45},
        {"unparsable returns raw", 123, "garbage", 123},
        {"whitespace tolerated", 50, "  * 10  ", 500},
    }
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            if got := applyFreqCalc(c.raw, c.expr); got != c.want {
                t.Errorf("applyFreqCalc(%v, %q) = %v, want %v", c.raw, c.expr, got, c.want)
            }
            // Cached path must agree with the fallback parse path
            freqCalcCache[c.expr] = parseFreqCalc(c.expr)
            if got := applyFreqCalc(c.raw, c.expr); got != c.want {
                t.Errorf("cached applyFreqCalc(%v, %q) = %v, want %v", c.raw, c.expr, got, c.want)
            }
        })
    }
}

func TestStatusToString(t *testing.T) {
    optidriveP2Bits := map[string]int{"Enabled": 0, "Tripped": 1, "Inhibited": 3}
    optidriveE3Bits := map[string]int{"Enabled": 0, "Tripped": 1}

    cases := []struct {
        name          string
        status        int
        statusBits    map[string]int
        enabledStatus int
        want          string
    }{
        // Integer-based status (no StatusBits defined)
        {"int-based no fault", 0, nil, 0, "Running"},
        {"int-based fault", 5, nil, 0, "Inhibited"},
        {"int-based empty map", 0, map[string]int{}, 0, "Running"},

        // Bit-based (OptidriveP2/E3 style)
        {"P2 stopped", 0, optidriveP2Bits, 0, "Stopped"},
        {"P2 running", 1 << 0, optidriveP2Bits, 0, "Running"},
        {"P2 tripped", 1 << 1, optidriveP2Bits, 0, "Tripped"},
        {"P2 tripped wins over enabled", 1<<0 | 1<<1, optidriveP2Bits, 0, "Tripped"},
        {"P2 inhibited", 1 << 3, optidriveP2Bits, 0, "NotReady"},
        {"P2 tripped wins over inhibited", 1<<1 | 1<<3, optidriveP2Bits, 0, "Tripped"},
        {"E3 running", 1 << 0, optidriveE3Bits, 0, "Running"},

        // GS44020 special path (EnabledStatus register value > 0)
        {"GS4 enabled", 1 << 0, map[string]int{"Inhibited": 3}, 1, "Running"},
        {"GS4 inhibited", 1 << 3, map[string]int{"Inhibited": 3}, 1, "NotReady"},
        {"GS4 enabledStatus set but bit0 clear", 0, map[string]int{"Inhibited": 3}, 2, "Stopped"},
        {"GS4 disabled falls back to bits", 0, map[string]int{"Inhibited": 3}, 0, "Stopped"},
    }
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            if got := statusToString(c.status, c.statusBits, c.enabledStatus); got != c.want {
                t.Errorf("statusToString(%#x, %v, %d) = %q, want %q", c.status, c.statusBits, c.enabledStatus, got, c.want)
            }
        })
    }
}

func TestGetDrivesForGroups(t *testing.T) {
    orig := appConfig
    defer func() { appConfig = orig }()
    appConfig = AppConfig{VFDs: []DriveConfig{
        {IP: "10.0.0.1", Group: "A"},
        {IP: "10.0.0.2", Group: "A"},
        {IP: "10.0.0.3", Group: "B"},
    }}

    if got := getDrivesForGroups(nil); len(got) != 3 {
        t.Errorf("empty groups should return all drives, got %d", len(got))
    }
    if got := getDrivesForGroups([]string{"A"}); len(got) != 2 {
        t.Errorf("group A should return 2 drives, got %d", len(got))
    }
    if got := getDrivesForGroups([]string{"A", "B"}); len(got) != 3 {
        t.Errorf("groups A+B should return 3 drives, got %d", len(got))
    }
    if got := getDrivesForGroups([]string{"missing"}); len(got) != 0 {
        t.Errorf("unknown group should return 0 drives, got %d", len(got))
    }
}

func TestFindDriveType(t *testing.T) {
    orig := ipToDrive
    defer func() { ipToDrive = orig }()
    ipToDrive = map[string]*DriveConfig{
        "10.0.0.1": {IP: "10.0.0.1", DriveType: "CFW500"},
    }

    if dt, ok := findDriveType("10.0.0.1"); !ok || dt != "CFW500" {
        t.Errorf("findDriveType known IP = (%q, %v), want (CFW500, true)", dt, ok)
    }
    if _, ok := findDriveType("10.0.0.99"); ok {
        t.Error("findDriveType unknown IP should return ok=false")
    }
}

func TestMarkDriveOffline(t *testing.T) {
    entry := map[string]interface{}{
        "ip":            "10.0.0.1",
        "status":        "Running",
        "actualSpeed":   45.0,
        "setSpeed":      45.0,
        "actualPercent": 75.0,
        "rpmSpeed":      487,
        "actualCfm":     25000,
        "current":       12.3,
        "clockwise":     0,
    }
    markDriveOffline(entry, "Unavailable")

    if entry["status"] != "Unavailable" {
        t.Errorf("status = %v, want Unavailable", entry["status"])
    }
    if entry["actualSpeed"] != 0.0 || entry["setSpeed"] != 0.0 || entry["current"] != 0.0 {
        t.Error("live float fields should be zeroed")
    }
    if entry["rpmSpeed"] != 0 || entry["actualCfm"] != 0 {
        t.Error("live int fields should be zeroed")
    }
    if entry["clockwise"] != 1 {
        t.Errorf("clockwise = %v, want 1", entry["clockwise"])
    }
    if entry["ip"] != "10.0.0.1" {
        t.Error("static fields must not be touched")
    }
}
