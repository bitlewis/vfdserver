VFD Server Controller

This is a server built in golang to control Optidrive E3 VFDs via Modbus TCP (using a waveshare POE to rs485 gateway).

config.json goes into /etc/vfd
index.html goes into /etc/vfd

Then, using go1.23.2 (current at the time of writing), simply go build vfdserver.go and place it in /usr/bin.
We are using supervisord to run and daemonize this. Config is also included (vfdserver-supervisord.conf).
