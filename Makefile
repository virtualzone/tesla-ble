all: clean linux macos

clean:
	rm -f build/*

linux: linux_amd64 linux_arm64

macos: macos_amd64 macos_arm64

linux_amd64:
	env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o build/tesla-ble_linux_amd64

linux_arm64:
	env CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-w -s" -o build/tesla-ble_linux_arm64

macos_amd64:
	env CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="-w -s" -o build/tesla-ble_macos_amd64

macos_arm64:
	env CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-w -s" -o build/tesla-ble_macos_arm64
