<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>BLU02 VFD Control</title>
    <style>
        body { font-family: Arial, sans-serif; background-color: #f4f7fc; margin: 0; padding: 0; }
        .container { width: 95%; margin: 20px auto; }
        h1 { text-align: center; color: #333; }
        .pod { margin-bottom: 10px; }
        .pod-title { cursor: pointer; font-size: 18px; font-weight: bold; background: #4CAF50; color: white; padding: 10px; border-radius: 5px; padding: 10px; }
        .drive-list { display: none; padding-left: 20px; }
        .drive-item { padding: 5px; background: #fff; margin: 5px 0; border-radius: 5px; display: flex; align-items: center; }
        input[type="checkbox"] { margin-right: 10px; }
        .select-all-container { margin-top: 10px; margin-bottom: 10px; font-size: 16px; }
        .status-box { padding: 4px; border-radius: 4px; display: inline-block; cursor: not-allowed; }
        .grey { background-color: #d3d3d3; color: #555; }
        .blue { background-color: #007bff; color: white; }
        .stale-data { background-color: #d3d3d3; }
        .control-panel { background-color: #fff; padding: 20px; border-radius: 10px; box-shadow: 0 0 10px rgba(0, 0, 0, 0.1); margin-top: 20px; }
        .control-panel button { padding: 10px 20px; border: none; border-radius: 5px; cursor: pointer; font-size: 16px; margin-right: 10px; }
        .control-panel button.freespin { background-color: #ffa500; color: white; }
        .control-panel button.fanhold { background-color: #f44336; color: white; }
        .control-panel button.set-speed { background-color: #128115; color: white; }
        .control-panel input[type="number"] { padding: 10px; border: 1px solid #999999; border-radius: 5px; width: 100px; }
        .control-events-panel { position: fixed; top: 0; right: 0; width: 300px; height: 100vh; background-color: #fff; padding: 20px; border-left: 1px solid #ddd; overflow-y: auto; }
        .control-events-panel h2 { margin-top: 0; }
        .control-event { padding: 10px; border-bottom: 1px solid #ddd; }
        .control-event:last-child { border-bottom: none; }
        .event-box { padding: 4px; border-radius: 4px; font-size: 12px; display: inline-block; cursor: not-allowed; }
        .devices-box { padding: 4px; border-radius: 4px; font-size: 10px; display: inline-block; cursor: not-allowed; }
        .timestamp {font-size: 14px; }
    </style>
    <script>
        let expandedPods = JSON.parse(localStorage.getItem("expandedPods")) || {};
        if (Object.keys(expandedPods).length === 0) {
            expandedPods = new Proxy({}, { get: () => true }); // Default all PODs to expanded
        }

        document.addEventListener("DOMContentLoaded", function() {
            const ws = new WebSocket('ws://10.33.10.52:8081/ws');
            ws.onmessage = function (event) {
                const data = JSON.parse(event.data);
                updateUI(data);
            };
        });

        function updateUI(data) {
            const container = document.getElementById('drive-container');
            const currentCheckboxStates = {}; // Store the current checkbox states

            // Store the current checkbox states before updating the data
            document.querySelectorAll(".drive-checkbox").forEach(cb => {
                currentCheckboxStates[cb.dataset.ip] = cb.checked;
            });

            // Organize drives by POD
            let pods = {};
            data.forEach(drive => {
                if (!pods[drive.pod]) {
                    pods[drive.pod] = [];
                }
                pods[drive.pod].push(drive);
            });

            container.innerHTML = '';  // Clear the container to update it

            // Create or update PODs
            for (let pod in pods) {
                let podDiv = document.createElement("div");
                podDiv.className = "pod";

                let podTitle = document.createElement("div");
                podTitle.className = "pod-title";
                podTitle.innerHTML = `POD ${pod}`;
                podTitle.addEventListener("click", function () {
                    const list = podDiv.querySelector(".drive-list");
                    list.style.display = (list.style.display === "block") ? "none" : "block";
                    expandedPods[pod] = list.style.display === "block";
                    localStorage.setItem("expandedPods", JSON.stringify(expandedPods));
                });

                let driveList = document.createElement("div");
                driveList.className = "drive-list";
                driveList.style.display = expandedPods[pod] ? "block" : "none";

                // Add the "Select All" checkbox inside the drive list
                let selectAllDiv = document.createElement("div");
                selectAllDiv.className = "select-all-container";
                let selectAllChecked = pods[pod].every(drive => currentCheckboxStates[drive.ip] === true); // Check if all drives are selected
                selectAllDiv.innerHTML = `<input type="checkbox" class="select-all" data-pod="${pod}" ${selectAllChecked ? 'checked' : ''}> Select All`;
                driveList.appendChild(selectAllDiv);

                pods[pod].forEach(drive => {
                    let driveItem = document.createElement("div");
                    driveItem.className = "drive-item";

                    // Use the previously stored checkbox state
                    let checked = currentCheckboxStates[drive.ip] || false;

                    let lastUpdated = new Date(drive.lastUpdated * 1000);
                    let currentTime = new Date();
                    let timeDiff = (currentTime - lastUpdated) / 1000;

                    let countdown = '';
                    if (timeDiff > 15) {
                        countdown = ` (Not updated for ${Math.floor(timeDiff)} seconds)`;
                        driveItem.classList.add("stale-data");
                    } else {
                        countdown = ``;
                        driveItem.classList.remove("stale-data");
                    }

                    // Uncheck and disable the checkbox if the drive status is Unknown
                    if (drive.status === 'Unknown') {
                        checked = false;
                    }

                    const statusText = drive.status === 'Unknown' ? 'Attempting Poll' :
                        drive.status === 'Waiting' ? 'Initialization' :
                        drive.status === 'Stopped' ? 'Freespin' :
                        drive.status === 'Running' && drive.setSpeed === 0 ? 'Fan Hold' :
                        drive.status === 'Running' ? 'Running' : '';

                    const statusColor = drive.status === 'Stopped' ? '#FFA500' :
                        drive.status === 'Running' && drive.setSpeed > 0 ? '#4CAF50' :
                        drive.status === 'Running' && drive.setSpeed === 0 ? '#f44336' :
                        '#007bff';

                    driveItem.innerHTML = `
                        <input type="checkbox" class="drive-checkbox" data-ip="${drive.ip}" data-pod="${pod}" ${checked ? 'checked' : ''} ${drive.status === 'Unknown' ? 'disabled' : ''}>
                        <span>
                            #${drive.fanNumber} - ${drive.ip} >> 
                            <span class="status-box grey" style="width: 60px; text-align: center;">${drive.setSpeed} Hz</span> 
                            <span class="status-box blue" style="width: 60px; text-align: center;">${drive.actualSpeed} Hz</span> 
                            <span class="status-box blue" style="width: 80px; text-align: center;">${drive.rpmSpeed} RPM</span> 
                            <span class="status-box blue" style="width: 50px; text-align: center;">${drive.current} A</span> 
                            <span class="status-box" style="background: ${statusColor}; color: white; text-align: center; padding: 4px; border-radius: 4px;">
                                ${statusText}${countdown}
                            </span>
                        </span>
                    `;

                    driveList.appendChild(driveItem);
                });
                podDiv.appendChild(podTitle);
                podDiv.appendChild(driveList);
                container.appendChild(podDiv);
            }

            // Reattach the 'Select All' checkbox functionality
            document.querySelectorAll(".select-all").forEach(checkbox => {
                checkbox.addEventListener("change", function () {
                    let pod = this.dataset.pod;
                    let checkboxes = document.querySelectorAll(`.drive-checkbox[data-pod='${pod}']`);
                    checkboxes.forEach(cb => {
                        if (!cb.disabled) {
                            cb.checked = this.checked;
                        }
                    });
                });
            });

            // Reattach the checkbox change event for individual drives
            document.querySelectorAll(".drive-checkbox").forEach(checkbox => {
                checkbox.addEventListener("change", function () {
                    let pod = this.dataset.pod;
                    let selectAllCheckbox = document.querySelector(`.select-all[data-pod='${pod}']`);
                    let allChecked = Array.from(document.querySelectorAll(`.drive-checkbox[data-pod='${pod}']`))
                        .every(cb => !cb.disabled && cb.checked);
                    selectAllCheckbox.checked = allChecked;
                });
            });
        }
        function selectAllDrives() {
            const selectAllCheckbox = document.getElementById('select-all');
            const checkboxes = document.querySelectorAll('.drive-checkbox');

            checkboxes.forEach(checkbox => {
                if (!checkbox.disabled) {
                    checkbox.checked = selectAllCheckbox.checked;
                }
            });
        }        
        let controlEvents = [];

        function sendControl(action, speed = null) {
            let selectedDrives = Array.from(document.querySelectorAll(".drive-checkbox:checked"))
                .map(cb => cb.dataset.ip);

            if (selectedDrives.length === 0) return alert("No drives selected.");

            let body = { drives: selectedDrives, action: action };
            if (speed) body.speed = parseFloat(speed);

            fetch('/control', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body)
            }).then(response => response.text())
        }

        function updateControlEvents() {
            fetch('/control-events')
                .then(response => response.json())
                .then(events => {
                    controlEvents = events;
                    updateControlEventsUI();
                });
        }

        function updateControlEventsUI() {
            const controlEventsDiv = document.getElementById('control-events');
            controlEventsDiv.innerHTML = '';
            controlEvents.slice().reverse().forEach(event => {
                const eventDiv = document.createElement('div');
                eventDiv.className = 'control-event';
                
                // Format timestamp as YYYY-MM-DD HH-MM-SS
                const timestamp = new Date(event.timestamp);
                const formattedDate = `${timestamp.getFullYear()}-${String(timestamp.getMonth() + 1).padStart(2, '0')}-${String(timestamp.getDate()).padStart(2, '0')} ${String(timestamp.getHours()).padStart(2, '0')}:${String(timestamp.getMinutes()).padStart(2, '0')}:${String(timestamp.getSeconds()).padStart(2, '0')}`;
                
                eventDiv.innerHTML = `
                    <div>
                        <span class="timestamp">${formattedDate}:</span> 
                        <span class="event-box" style="background-color: #007bff; color: white;">
                        ${event.action === 'SetSpeed' ? `Set ${event.speed} Hz` : event.action}</span>
                    </div>
                    ${event.drives.map(drive => `
                        <span class="devices-box" style="background-color: ${drive.success ? '#4CAF50' : '#f44336'}; color: white;">${drive.ip}${drive.error ? ` - ${drive.error}` : ''}</span>
                    `).join('')}
                `;
                controlEventsDiv.appendChild(eventDiv);
            });
        }

        setInterval(updateControlEvents, 1000);        
    </script>
</head>
<body>
    <div class="container">
        <h1>BLU02 VFD Control Panel</h1>
        <div class="control-panel">
            <input type="number" id="speed-value" step=".1" placeholder="Set Speed" required value="1">
            <button class="set-speed" onclick="sendControl('SetSpeed', document.getElementById('speed-value').value)">Set Speed</button>
            <button class="freespin" onclick="sendControl('Freespin')">Freespin</button>
            <button class="fanhold" onclick="sendControl('Fanhold')">Fan Hold</button>
            <input type="checkbox" id="select-all" onclick="selectAllDrives()">Select All Active Drives
        </div>
        <br>
        <div id="drive-container"></div>
    </div>
    <div class="control-events-panel">
        <h2>Control Events</h2>
        <div id="control-events"></div>
    </div>
</body>
</html>
