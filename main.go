package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/teslamotors/vehicle-command/pkg/cache"
	"github.com/teslamotors/vehicle-command/pkg/connector/ble"
	"github.com/teslamotors/vehicle-command/pkg/protocol"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/universalmessage"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/vcsec"
	"github.com/teslamotors/vehicle-command/pkg/vehicle"
)

type cmdFunction func(http.ResponseWriter, *http.Request, *vehicle.Vehicle, map[string]interface{}) error

var sessionCache = cache.New(5)
var commands = map[string]cmdFunction{
	"pair":              cmdPairVehicle,
	"wake_up":           cmdWakeUp,
	"set_charging_amps": cmdSetChargingAmps,
	"set_soc_limit":     cmdSetSocLimit,
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

func execCommand(w http.ResponseWriter, r *http.Request, command string) {

}

func handleCommand(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	vin := vars["vin"]
	command := vars["command"]

	if vin == "" {
		sendBadRequest(w)
		return
	}

	cmdFunc, ok := commands[command]
	if !ok {
		sendNotFound(w)
		return
	}

	var body map[string]interface{} = nil
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		log.Printf("Error decoding body: %s\n", err)
		sendBadRequest(w)
		return
	}

	log.Printf("Executing command %s for VIN %s ...\n", command, vin)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := ble.NewConnection(ctx, vin)
	if err != nil {
		log.Printf("Failed to connect to vehicle: %s", err)
		sendInternalServerError(w)
		return
	}
	defer conn.Close()

	car, err := vehicle.NewVehicle(conn, GetConfig().PrivateKey, sessionCache)
	if err != nil {
		log.Printf("Failed to connect to vehicle: %s", err)
		sendInternalServerError(w)
		return
	}

	if err := car.Connect(ctx); err != nil {
		log.Printf("Failed to connect to vehicle: %s", err)
		sendInternalServerError(w)
		return
	}
	defer car.Disconnect()

	if command != "pair" {
		var domains []universalmessage.Domain = nil
		if command == "wake_up" {
			domains = []universalmessage.Domain{protocol.DomainVCSEC}
		}
		if err := car.StartSession(ctx, domains); err != nil {
			if strings.Contains(err.Error(), "context deadline exceeded") && command != "wake_up" {
				log.Println("Vehicle is asleep, trying to wake it first...")
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := car.StartSession(ctx, []universalmessage.Domain{protocol.DomainVCSEC}); err != nil {
					log.Printf("Could not wake vehicle: %s\n", err)
					sendInternalServerError(w)
					return
				}
			} else {
				log.Printf("Failed to perform handshake with vehicle: %s", err)
				sendInternalServerError(w)
				return
			}
		}
	}
	defer car.UpdateCachedSessions(sessionCache)

	cancel()

	tries := 1
	for tries <= 3 {
		if tries > 1 {
			log.Printf("Retry %d of command %s for VIN %s ...\n", tries, command, vin)
		}
		if err = cmdFunc(w, r, car, body); err != nil {
			log.Printf("Failed to process command %s: %s\n", command, err)
			tries++
		} else {
			log.Printf("Successfully processed command %s\n", command)
			sendJSON(w, true)
			return
		}
	}
	log.Printf("Giving up on command %s for VIN %s after too many reties\n", command, vin)
	sendBadRequest(w)
}

func cmdPairVehicle(w http.ResponseWriter, r *http.Request, car *vehicle.Vehicle, body map[string]interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := car.SendAddKeyRequest(ctx, GetConfig().PublicKey, true, vcsec.KeyFormFactor_KEY_FORM_FACTOR_UNKNOWN); err != nil {
		return fmt.Errorf("failed to send add key request: %s", err)
	}
	return nil
}

func cmdWakeUp(w http.ResponseWriter, r *http.Request, car *vehicle.Vehicle, body map[string]interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := car.Wakeup(ctx); err != nil {
		return fmt.Errorf("failed to wake up vehicle: %s", err)
	}
	return nil
}

func cmdSetChargingAmps(w http.ResponseWriter, r *http.Request, car *vehicle.Vehicle, body map[string]interface{}) error {
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

func cmdSetSocLimit(w http.ResponseWriter, r *http.Request, car *vehicle.Vehicle, body map[string]interface{}) error {
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

func cmdChargeStart(w http.ResponseWriter, r *http.Request, car *vehicle.Vehicle, body map[string]interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := car.ChargeStart(ctx); err != nil {
		return fmt.Errorf("failed to start charging: %s", err)
	}
	return nil
}

func cmdChargeStop(w http.ResponseWriter, r *http.Request, car *vehicle.Vehicle, body map[string]interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := car.ChargeStop(ctx); err != nil {
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
