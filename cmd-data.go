package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/teslamotors/vehicle-command/pkg/vehicle"
)

type getDataFunction func(*vehicle.Vehicle) (interface{}, error)

var dataCommands = map[string]getDataFunction{
	"get_soc":           getSoc,
	"get_soc_limit":     getLimitSoc,
	"get_battery_range": getBatteryRange,
	"get_charge_state":  getChargeState,
}

func handleGetDataCommand(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	vin := vars["vin"]
	command := vars["command"]

	if vin == "" {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	res, err := execDataCommand(vin, command)
	if err != nil {
		if errors.Is(err, errCmdNotFound) {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		log.Printf("could not exec command %s: %s\n", command, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	sendJSON(w, res)
}

func execDataCommand(vin string, command string) (interface{}, error) {
	cmdFunc, ok := dataCommands[command]
	if !ok {
		return nil, errCmdNotFound
	}

	log.Printf("Executing get data command %s for VIN %s ...\n", command, vin)

	car, conn, err := prepareConnection(vin, command)
	if err != nil {
		return nil, fmt.Errorf("could not prepare vehicle connection: %s", err)
	}
	defer conn.Close()
	defer car.Disconnect()

	res, err := cmdFunc(car)
	if err != nil {
		return nil, fmt.Errorf("could not get data: %s", err)
	}
	return res, nil
}

func getSoc(car *vehicle.Vehicle) (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, err := car.GetState(ctx, vehicle.StateCategoryCharge)
	if err != nil {
		return 0, fmt.Errorf("failed to get state: %s", err)
	}
	return data.GetChargeState().GetBatteryLevel(), nil
}

func getLimitSoc(car *vehicle.Vehicle) (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, err := car.GetState(ctx, vehicle.StateCategoryCharge)
	if err != nil {
		return 0, fmt.Errorf("failed to get state: %s", err)
	}
	return data.GetChargeState().GetChargeLimitSoc(), nil
}

func getBatteryRange(car *vehicle.Vehicle) (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, err := car.GetState(ctx, vehicle.StateCategoryCharge)
	if err != nil {
		return 0, fmt.Errorf("failed to get state: %s", err)
	}
	return data.GetChargeState().GetBatteryRange(), nil
}

func getChargeState(car *vehicle.Vehicle) (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, err := car.GetState(ctx, vehicle.StateCategoryCharge)
	if err != nil {
		return "A", fmt.Errorf("failed to get state: %s", err)
	}
	state := data.GetChargeState().GetChargingState()
	if state.GetCharging() != nil {
		return "C", nil
	}
	if state.GetStopped() != nil || state.GetNoPower() != nil || state.GetComplete() != nil {
		return "B", nil
	}
	return "A", nil
}
