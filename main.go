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
	"time"

	"github.com/gorilla/mux"
	"github.com/teslamotors/vehicle-command/pkg/cache"
	"github.com/teslamotors/vehicle-command/pkg/connector/ble"
	"github.com/teslamotors/vehicle-command/pkg/protocol"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/universalmessage"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/vcsec"
	"github.com/teslamotors/vehicle-command/pkg/vehicle"
)

var sessionCache = cache.New(5)
var commands = map[string]func(http.ResponseWriter, *http.Request, *vehicle.Vehicle, map[string]interface{}){
	"pair":              pairVehicle,
	"wake_up":           cmdWakeUp,
	"set_charging_amps": cmdSetChargingAmps,
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

	log.Printf("Tesla BLE: Executing command %s for VIN %s ...\n", command, vin)

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
			log.Printf("Failed to perform handshake with vehicle: %s", err)
			sendInternalServerError(w)
			return
		}
	}
	defer car.UpdateCachedSessions(sessionCache)

	cancel()

	cmdFunc(w, r, car, body)
}

func pairVehicle(w http.ResponseWriter, r *http.Request, car *vehicle.Vehicle, body map[string]interface{}) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := car.SendAddKeyRequest(ctx, GetConfig().PublicKey, true, vcsec.KeyFormFactor_KEY_FORM_FACTOR_UNKNOWN); err != nil {
		log.Printf("Failed to send add key request: %s\n", err)
	}
}

func cmdWakeUp(w http.ResponseWriter, r *http.Request, car *vehicle.Vehicle, body map[string]interface{}) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := car.Wakeup(ctx); err != nil {
		log.Printf("Failed to wake up vehicle: %s\n", err)
	}
}

func cmdSetChargingAmps(w http.ResponseWriter, r *http.Request, car *vehicle.Vehicle, body map[string]interface{}) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	chargingAmpsString, ok := body["charging_amps"].(string)
	if !ok {
		log.Printf("Failed to find charging_amps in request body\n")
		sendBadRequest(w)
		return
	}

	chargingAmps, err := strconv.ParseInt(chargingAmpsString, 10, 32)
	if err != nil {
		log.Printf("Failed to parse charging_amps to int: %s\n", err)
		sendBadRequest(w)
		return
	}

	if err := car.SetChargingAmps(ctx, int32(chargingAmps)); err != nil {
		log.Printf("Failed to set charging amps: %s\n", err)
	}
}

func cmdChargeStart(w http.ResponseWriter, r *http.Request, car *vehicle.Vehicle, body map[string]interface{}) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := car.ChargeStart(ctx); err != nil {
		log.Printf("Failed to start charging: %s\n", err)
	}
}

func cmdChargeStop(w http.ResponseWriter, r *http.Request, car *vehicle.Vehicle, body map[string]interface{}) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := car.ChargeStop(ctx); err != nil {
		log.Printf("Failed to stop charging: %s\n", err)
	}
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
