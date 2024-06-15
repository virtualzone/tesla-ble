# Tesla BLE
A simple HTTP server sending commands controlling your Tesla's charging process via BLE.

## Set up
1. Generate private key:

   ```openssl ecparam -genkey -name prime256v1 -noout > private.pem```
1. Generate public key:

   ```openssl ec -in private.pem -pubout > public.pem```
1. Start container using Docker Compose:

   ```yaml
   services:
     tesla-ble:
        image: ghcr.io/virtualzone/tesla-ble:latest
        container_name: tesla-ble
        restart: always
        volumes:
           - './private.pem:/app/private.pem'
           - './public.pem:/app/public.pem'
           - '/var/run/dbus:/var/run/dbus'
           - '/run/dbus:/run/dbus:ro'
        privileged: true
        network_mode: host
        cap_add:
           - NET_ADMIN
           - SYS_ADMIN
    ```
1. Get into your car, have your key card ready, call the following endpoint to send a BLE pairing request to your Tesla and follow the instructions on screen (replace VEHICLE_VIN with your vehicle's VIN):

   ```
   curl -X POST localhost:8080/api/1/vehicles/VEHICLE_VIN/command/pair
   ```
1. After successful paring, you can use the endpoints listed below.

## Available endpoints
### Send BLE paring request
   ```
   curl -X POST \
   http://localhost:8080/api/1/vehicles/VEHICLE_VIN/command/pair
   ```

### Wake vehicle
   ```
   curl -X POST \
   http://localhost:8080/api/1/vehicles/VEHICLE_VIN/command/wakeup
   ```

### Set charging amps
   ```
   curl -X POST \
   -d '{"charging_amps": "16"}' \
   http://localhost:8080/api/1/vehicles/VEHICLE_VIN/command/set_charging_amps
   ```

### Set charging limit
   ```
   curl -X POST \
   -d '{"soc_limit": "70"}' \
   http://localhost:8080/api/1/vehicles/VEHICLE_VIN/command/set_soc_limit
   ```

### Start charging
   ```
   curl -X POST \
   http://localhost:8080/api/1/vehicles/VEHICLE_VIN/command/charge_start
   ```

### Stop charging
   ```
   curl -X POST \
   http://localhost:8080/api/1/vehicles/VEHICLE_VIN/command/charge_stop
   ```

### Set charging state
   ```
   curl -X POST \
   -d '{"enable": "true"}' \
   http://localhost:8080/api/1/vehicles/VEHICLE_VIN/command/charge
   ```

## Using with evcc
```yaml
vehicles:
  - name: tesla
    type: custom
    title: Model Y
    capacity: 78
    wakeup:
      source: http
      uri: "http://localhost:8080/api/1/vehicles/VEHICLE_VIN/command/wake_up"
      method: POST
      body: ''
    maxcurrent:
      source: http
      uri: "http://localhost:8080/api/1/vehicles/VEHICLE_VIN/command/set_charging_amps"
      method: POST
      body: '{"charging_amps": "{{.maxcurrent}}"}'
    chargeenable:
      source: http
      uri: "http://localhost:8080/api/1/vehicles/VEHICLE_VIN/command/charge"
      method: POST
      body: '{"enable": "{{.chargeenable}}"}'
```