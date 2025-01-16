package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/vcsec"
	"github.com/teslamotors/vehicle-command/pkg/vehicle"
)

type cmdFunction func(*vehicle.Vehicle, map[string]interface{}) error

var commands = map[string]cmdFunction{
	"pair":              cmdPairVehicle,
	"wake_up":           cmdWakeUp,
	"set_charging_amps": cmdSetChargingAmps,
	"set_soc_limit":     cmdSetSocLimit,
	"charge":            cmdChargeEnable,
	"charge_start":      cmdChargeStart,
	"charge_stop":       cmdChargeStop,
}

func handleExecCommand(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	vin := vars["vin"]
	command := vars["command"]

	if vin == "" {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	var body map[string]interface{} = nil
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		log.Printf("Error decoding body: %s\n", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	if needWakeUp(command) {
		if err := execCommand(vin, "wake_up", body); err != nil {
			log.Printf("Waking vehicle failed, giving up: %s\n", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		time.Sleep(5 * time.Second)
	}

	if err := execCommand(vin, command, body); err != nil {
		if errors.Is(err, errCmdNotFound) {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		log.Printf("could not exec command %s: %s\n", command, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	sendJSON(w, true)
}

func execCommand(vin string, command string, body map[string]interface{}) error {
	cmdFunc, ok := commands[command]
	if !ok {
		return errCmdNotFound
	}

	log.Printf("Executing command %s for VIN %s ...\n", command, vin)

	car, conn, err := prepareConnection(vin, command)
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
