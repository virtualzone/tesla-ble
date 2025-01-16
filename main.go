package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/teslamotors/vehicle-command/pkg/connector/ble"
	"github.com/teslamotors/vehicle-command/pkg/protocol"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/universalmessage"
	"github.com/teslamotors/vehicle-command/pkg/vehicle"
)

var errCmdNotFound = errors.New("command not found")

func main() {
	log.Println("Starting virtualzone.de Tesla BLE Controller...")
	serveHTTP()
}

func prepareConnection(vin string, command string) (*vehicle.Vehicle, *ble.Connection, error) {
	timeout := 30 * time.Second
	if strings.Index(command, "get_") == 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	conn, err := ble.NewConnection(ctx, vin)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create BLE connection to vehicle: %s", err)
	}

	car, err := vehicle.NewVehicle(conn, GetConfig().PrivateKey, nil)
	if err != nil {
		conn.Close()
		return nil, conn, fmt.Errorf("failed to create vehicle: %s", err)
	}

	if err := car.Connect(ctx); err != nil {
		conn.Close()
		return nil, conn, fmt.Errorf("failed to connect to vehicle: %s", err)
	}

	if command != "pair" {
		var domains []universalmessage.Domain = nil
		if command == "wake_up" {
			domains = []universalmessage.Domain{protocol.DomainVCSEC}
		}
		if err := car.StartSession(ctx, domains); err != nil {
			car.Disconnect()
			conn.Close()
			return nil, conn, fmt.Errorf("failed to perform handshake with vehicle: %s", err)
		}
	}

	return car, conn, nil
}

func needWakeUp(command string) bool {
	var wakeCommands = []string{"wake_up", "pair"}
	return !slices.Contains(wakeCommands, command)
}
