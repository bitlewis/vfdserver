package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
	"sync"

	"github.com/grid-x/modbus"
	"github.com/gorilla/websocket"
)

type DriveConfig struct {
	IP           string `json:"IP"`
	Port         int    `json:"Port"`
	Unit         int    `json:"Unit"`
	DefaultSpeed int    `json:"DefaultSpeed"`
	Pod          int    `json:"Pod"`
	FanNumber    int    `json:"FanNumber"`
	RpmToHz      int    `json:"RpmHz"`
	LastPull     int64  `json:"-"`
}

type VFDConfig map[string][]DriveConfig

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

var controlEvents []ControlEvent

var vfdConfig VFDConfig
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func main() {
	var err error
	vfdConfig, err = loadConfig("/etc/vfd/config.json")
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/", handleLivePage)
	http.HandleFunc("/ws", handleWebSocket)
	http.HandleFunc("/control", handleControl)
	http.HandleFunc("/control-events", handleControlEvents)

	log.Println("VFD Control Server v2.1 by Louis Valois - for AAIMDC BLU02 Site\nWeb server started on http://10.33.10.53")
	log.Fatal(http.ListenAndServe("10.33.10.53:80", nil))
}

func loadConfig(filename string) (VFDConfig, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("could not open config file: %w", err)
	}
	defer file.Close()

	var config VFDConfig
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		return nil, fmt.Errorf("could not parse config file: %w", err)
	}

	// Set LastPull to the current time for all drives
	for key, drives := range config {
		for i, drive := range drives {
			drive.LastPull = time.Now().Unix()
			config[key][i] = drive
		}
	}

	return config, nil
}

func setupModbusClient(ip string, port int) (modbus.Client, *modbus.TCPClientHandler, error) {
	handler := modbus.NewTCPClientHandler(fmt.Sprintf("%s:%d", ip, port))
	handler.Timeout = 1 * time.Second
	handler.SlaveID = 1
	err := handler.Connect()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to Modbus server %s: %v", ip, err)
	}
	client := modbus.NewClient(handler)
	return client, handler, nil
}

func getDriveStatus(client modbus.Client) (int, float64, float64, float64, error) {
	result, err := client.ReadInputRegisters(0, 9)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("failed to read status and speed: %v", err)
	}
	if len(result) < 18 {
		return 0, 0, 0, 0, fmt.Errorf("invalid response length")
	}

	status := int(result[0])<<8 | int(result[1])
	setspeed := float64(int(result[2])<<8 | int(result[3])) / 10.0
	actualSpeed := float64(int(result[12])<<8 | int(result[13])) / 10.0
	current := float64(int(result[14])<<8|int(result[15])) / 10.0

	return status, setspeed, actualSpeed, current, nil
}

func statusToString(status int) string {
	switch status {
	case 1:
		return "Running"
	case 0:
		return "Stopped"
	default:
		return "Unknown"
	}
}

func fanStop(client modbus.Client) error {
	_, err := client.WriteSingleRegister(0, 0)
	return err
}

func fanStart(client modbus.Client) error {
	_, err := client.WriteSingleRegister(0, 1)
	return err
}

func freeSpin(client modbus.Client) error {
	_, err := client.WriteSingleRegister(0, 0)
	_, err = client.WriteSingleRegister(1, 0)
	return err
}

func setFanSpeed(client modbus.Client, setspeed float64) error {
	actualSpeedSet := int(setspeed * 10)
	_, err := client.WriteSingleRegister(0, 1)
	_, err = client.WriteSingleRegister(1, uint16(actualSpeedSet))
	return err
}

func fanHold(client modbus.Client) error {
	_, err := client.WriteSingleRegister(0, 1)
	_, err = client.WriteSingleRegister(1, 0)
	return err
}

func handleLivePage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "/etc/vfd/index.html")
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println(err)
        return
    }
    defer conn.Close()

    // Send initial data with Unknown status
    initialData := []map[string]interface{}{}
    for _, drives := range vfdConfig {
        for _, drive := range drives {
            initialData = append(initialData, map[string]interface{}{
                "pod":         drive.Pod,
                "fanNumber":   drive.FanNumber,
                "ip":          drive.IP,
                "setSpeed":    0.0,
                "actualSpeed": 0.0,
                "rpmSpeed":    0,
                "current":     0.0,
                "status":      "Waiting",
                "lastUpdated": time.Now().Unix(),
            })
        }
    }

    // Send initial state immediately
    if err := conn.WriteJSON(initialData); err != nil {
        log.Println(err)
        return
    }

    // Start background updates
    go func() {
        for {
            data := getDrivesData()
            if err := conn.WriteJSON(data); err != nil {
                log.Println(err)
                return
            }
            time.Sleep(1 * time.Second)
        }
    }()

    // Keep the connection open
    for {
        _, _, err := conn.ReadMessage()
        if err != nil {
            log.Println(err)
            return
        }
    }
}

func getDrivesData() []map[string]interface{} {
	var drivesData []map[string]interface{}

	// Initialize drivesData with all drives from the configuration file
	for _, drives := range vfdConfig {
		for _, drive := range drives {
			drivesData = append(drivesData, map[string]interface{}{
				"pod":         drive.Pod,
				"fanNumber":   drive.FanNumber,
				"ip":          drive.IP,
				"setSpeed":    0.0,
				"actualSpeed": 0.0,
				"rpmSpeed":    0,
				"current":     0.0,
				"status":      "Unknown",
				"lastUpdated": drive.LastPull, // Initialize with the last successful pull timestamp
			})
		}
	}

	// Update data points for each drive in parallel
	var wg sync.WaitGroup
	for i, driveData := range drivesData {
		wg.Add(1)
		go func(i int, driveData map[string]interface{}) {
			defer wg.Done()
			ip := driveData["ip"].(string)
			for _, drives := range vfdConfig {
				for _, drive := range drives {
					if drive.IP == ip {
						client, handler, err := setupModbusClient(drive.IP, drive.Port)
						if err != nil {
							log.Println(err)
							return
						}
						defer handler.Close()

						status, setspeed, actualSpeed, current, err := getDriveStatus(client)
						if err != nil {
							log.Println(err)
							return
						}

						drivesData[i] = map[string]interface{}{
							"pod":         driveData["pod"],
							"fanNumber":   driveData["fanNumber"],
							"ip":          driveData["ip"],
							"setSpeed":    setspeed,
							"actualSpeed": actualSpeed,
							"rpmSpeed":    int(actualSpeed * 30),
							"current":     current,
							"status":      statusToString(status),
							"lastUpdated": time.Now().Unix(), // Update the lastUpdated field only on successful Modbus poll
						}

						// Update the LastPull field in the DriveConfig struct
						for j, d := range drives {
							if d.IP == ip {
								drives[j].LastPull = time.Now().Unix()
							}
						}
					}
				}
			}
		}(i, driveData)
	}

	wg.Wait()

	return drivesData
}

func handleControlEvents(w http.ResponseWriter, r *http.Request) {
    events := make([]map[string]interface{}, len(controlEvents))
    for i, event := range controlEvents {
        events[i] = map[string]interface{}{
            "timestamp": event.Timestamp.Format(time.RFC3339),
            "action":    event.Action,
            "speed":     event.Speed,
            "drives":    event.Drives,
        }
    }
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
	if controlData.Action != "Freespin" && controlData.Action != "Fanhold" && controlData.Action != "SetSpeed" && controlData.Action != "Restart" && controlData.Action != "Stop" {
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

    for _, ip := range controlData.Drives {
        driveInfo := DriveEventInfo{IP: ip, Success: true}
        
        // Find drive config and execute control
        for _, drives := range vfdConfig {
            for _, drive := range drives {
                if drive.IP == ip {
                    client, handler, err := setupModbusClient(drive.IP, drive.Port)
                    if err != nil {
                        driveInfo.Success = false
                        driveInfo.Error = fmt.Sprintf("Connection failed: %v", err)
                        continue
                    }
                    defer handler.Close()

                    // Execute control action and record result
                    if err := executeControl(client, controlData.Action, controlData.Speed); err != nil {
                        driveInfo.Success = false
                        driveInfo.Error = err.Error()
                    }
                    
                    event.Drives = append(event.Drives, driveInfo)
                }
            }
        }
    }

    // Log the event
    controlEvents = append(controlEvents, event)
    if len(controlEvents) > 100 {
        controlEvents = controlEvents[1:]
    }

    w.Write([]byte("Control action processed successfully"))
}

func executeControl(client modbus.Client, action string, speed float64) error {
    switch action {
    case "Restart":
        return fanStart(client)				
    case "Stop":
        return fanStop(client)		
    case "Fanhold":
        return fanHold(client)
    case "Freespin":
        return freeSpin(client)
    case "SetSpeed":
        return setFanSpeed(client, speed)
    default:
        return fmt.Errorf("invalid action: %s", action)
    }
}
