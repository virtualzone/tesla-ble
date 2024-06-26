package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/teslamotors/vehicle-command/pkg/connector/ble"
	"github.com/teslamotors/vehicle-command/pkg/protocol"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/universalmessage"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/vcsec"
	"github.com/teslamotors/vehicle-command/pkg/vehicle"
)

type cmdFunction func(*vehicle.Vehicle, map[string]interface{}) error

// var sessionCache = cache.New(5)
var commands = map[string]cmdFunction{
	"pair":              cmdPairVehicle,
	"wake_up":           cmdWakeUp,
	"set_charging_amps": cmdSetChargingAmps,
	"set_soc_limit":     cmdSetSocLimit,
	"charge":            cmdChargeEnable,
	"charge_start":      cmdChargeStart,
	"charge_stop":       cmdChargeStop,
}

func main() {
	log.Println("Starting chargebot.io Tesla BLE Controller...")
	serveHTTP()
}

func sendBadRequest(w http.ResponseWriter) {
	w.WriteHeader(http.StatusBadRequest)
}

func sendNotFound(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
}

func sendInternalServerError(w http.ResponseWriter) {
	w.WriteHeader(http.StatusInternalServerError)
}

func sendJSON(w http.ResponseWriter, v interface{}) {
	json, err := json.Marshal(v)
	if err != nil {
		log.Println(err)
		sendInternalServerError(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(json)
}

func prepareConnection(vin string, command string) (error, *vehicle.Vehicle, *ble.Connection) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := ble.NewConnection(ctx, vin)
	if err != nil {
		return fmt.Errorf("failed to create BLE connection to vehicle: %s", err), nil, nil
	}

	//car, err := vehicle.NewVehicle(conn, GetConfig().PrivateKey, sessionCache)
	car, err := vehicle.NewVehicle(conn, GetConfig().PrivateKey, nil)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to create vehicle: %s", err), nil, conn
	}

	if err := car.Connect(ctx); err != nil {
		conn.Close()
		return fmt.Errorf("failed to connect to vehicle: %s", err), nil, conn
	}

	if command != "pair" {
		var domains []universalmessage.Domain = nil
		if command == "wake_up" {
			domains = []universalmessage.Domain{protocol.DomainVCSEC}
		}
		if err := car.StartSession(ctx, domains); err != nil {
			car.Disconnect()
			conn.Close()
			return fmt.Errorf("failed to perform handshake with vehicle: %s", err), nil, conn
		}
	}
	//defer car.UpdateCachedSessions(sessionCache)

	return nil, car, conn
}

func retryCommand(vin string, command string, car *vehicle.Vehicle, cmdFunc cmdFunction, body map[string]interface{}) error {
	tries := 1
	for tries <= 3 {
		if tries > 1 {
			log.Printf("Retry %d of command %s for VIN %s ...\n", tries, command, vin)
		}
		if err := cmdFunc(car, body); err != nil {
			log.Printf("Failed to process command %s: %s\n", command, err)
			tries++
		} else {
			log.Printf("Successfully processed command %s\n", command)
			return nil
		}
	}
	log.Printf("Giving up on command %s for VIN %s after too many reties\n", command, vin)
	return errors.New("too many retries")
}

func execCommand(vin string, command string, body map[string]interface{}) error {
	cmdFunc, ok := commands[command]
	if !ok {
		return fmt.Errorf("invalid command %s", command)
	}

	log.Printf("Executing command %s for VIN %s ...\n", command, vin)

	err, car, conn := prepareConnection(vin, command)
	if err != nil {
		return fmt.Errorf("could not prepare vehicle connection: %s", err)
	}
	defer conn.Close()
	defer car.Disconnect()

	if err := retryCommand(vin, command, car, cmdFunc, body); err != nil {
		return fmt.Errorf("retrying command %s failed: %s", command, err)
	}
	return nil
}

func handleCommand(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	vin := vars["vin"]
	command := vars["command"]

	if vin == "" {
		sendBadRequest(w)
		return
	}

	var body map[string]interface{} = nil
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		log.Printf("Error decoding body: %s\n", err)
		sendBadRequest(w)
		return
	}

	if command != "wake_up" && command != "pair" {
		if err := execCommand(vin, "wake_up", body); err != nil {
			log.Printf("Waking vehicle failed, giving up: %s\n", err)
			sendInternalServerError(w)
			return
		}
		time.Sleep(5 * time.Second)
	}

	if err := execCommand(vin, command, body); err != nil {
		log.Printf("could not exec command %s: %s\n", command, err)
		sendInternalServerError(w)
		return
	}
	sendJSON(w, true)
}

func cmdPairVehicle(car *vehicle.Vehicle, body map[string]interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := car.SendAddKeyRequest(ctx, GetConfig().PublicKey, true, vcsec.KeyFormFactor_KEY_FORM_FACTOR_UNKNOWN); err != nil {
		return fmt.Errorf("failed to send add key request: %s", err)
	}
	return nil
}

func cmdWakeUp(car *vehicle.Vehicle, body map[string]interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := car.Wakeup(ctx); err != nil {
		return fmt.Errorf("failed to wake up vehicle: %s", err)
	}
	return nil
}

func cmdSetChargingAmps(car *vehicle.Vehicle, body map[string]interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	chargingAmpsString, ok := body["charging_amps"].(string)
	if !ok {
		return fmt.Errorf("failed to find charging_amps in request body")
	}

	chargingAmps, err := strconv.ParseInt(chargingAmpsString, 10, 32)
	if err != nil {
		return fmt.Errorf("failed to parse charging_amps to int: %s", err)
	}

	if err := car.SetChargingAmps(ctx, int32(chargingAmps)); err != nil {
		return fmt.Errorf("failed to set charging amps: %s", err)
	}
	return nil
}

func cmdSetSocLimit(car *vehicle.Vehicle, body map[string]interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	socLimitString, ok := body["soc_limit"].(string)
	if !ok {
		return fmt.Errorf("failed to find soc_limit in request body")
	}

	socLimit, err := strconv.ParseInt(socLimitString, 10, 32)
	if err != nil {
		return fmt.Errorf("failed to parse soc_limit to int: %s", err)
	}

	if err := car.ChangeChargeLimit(ctx, int32(socLimit)); err != nil {
		return fmt.Errorf("failed to set soc limit: %s", err)
	}
	return nil
}

func cmdChargeEnable(car *vehicle.Vehicle, body map[string]interface{}) error {
	enable, ok := body["enable"].(string)
	if !ok {
		return fmt.Errorf("failed to find enable in request body")
	}

	if enable == "true" {
		return cmdChargeStart(car, body)
	} else {
		return cmdChargeStop(car, body)
	}
}

func cmdChargeStart(car *vehicle.Vehicle, body map[string]interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := car.ChargeStart(ctx); err != nil {
		if strings.Contains(err.Error(), "already_started") || strings.Contains(err.Error(), "is_charging") {
			return nil
		}
		return fmt.Errorf("failed to start charging: %s", err)
	}
	return nil
}

func cmdChargeStop(car *vehicle.Vehicle, body map[string]interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := car.ChargeStop(ctx); err != nil {
		if strings.Contains(err.Error(), "not_charging") {
			return nil
		}
		return fmt.Errorf("failed to stop charging: %s", err)
	}
	return nil
}

func serveHTTP() {
	router := mux.NewRouter()
	router.HandleFunc("/api/1/vehicles/{vin}/command/{command}", handleCommand).Methods("POST")

	httpServer := &http.Server{
		Addr:         fmt.Sprintf("0.0.0.0:%d", GetConfig().Port),
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      router,
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil {
			log.Fatal(err)
			os.Exit(-1)
		}
	}()
	log.Println("HTTP Server listening")

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
	log.Println("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()
	httpServer.Shutdown(ctx)
}
