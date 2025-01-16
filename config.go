package main

import (
	"crypto/ecdh"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/teslamotors/vehicle-command/pkg/protocol"
)

type Config struct {
	Port       int
	PrivateKey protocol.ECDHPrivateKey
	PublicKey  *ecdh.PublicKey
	Username   string
	Password   string
}

var _configInstance *Config
var _configOnce sync.Once

func GetConfig() *Config {
	_configOnce.Do(func() {
		_configInstance = &Config{}
		_configInstance.ReadConfig()
	})
	return _configInstance
}

func (c *Config) ReadConfig() {
	port, err := strconv.Atoi(c.getEnv("PORT", "8080"))
	if err != nil {
		log.Panicln("PORT must be numeric")
	}
	c.Port = port

	c.Username = c.getEnv("USERNAME", "")
	c.Password = c.getEnv("PASSWORD", "")

	privateKeyFile := c.getEnv("PRIVATE_KEY", "./private.pem")
	if privateKeyFile == "" {
		log.Panicln("Need to specify PRIVATE_KEY")
	}
	if strings.Index(privateKeyFile, "http://") == 0 || strings.Index(privateKeyFile, "https://") == 0 {
		if err := GetCacheHttpFile(privateKeyFile, "/tmp/private.pem"); err != nil {
			log.Panicf("Could not load private key file via http: %s\n", err.Error())
		}
		privateKeyFile = "/tmp/private.pem"
	}
	privateKey, err := protocol.LoadPrivateKey(privateKeyFile)
	if err != nil {
		log.Panicf("Could not load private key: %s\n", err.Error())
	}
	c.PrivateKey = privateKey

	publicKeyFile := c.getEnv("PUBLIC_KEY", "./public.pem")
	if publicKeyFile == "" {
		log.Panicln("Need to specify PUBLIC_KEY")
	}
	if strings.Index(publicKeyFile, "http://") == 0 || strings.Index(publicKeyFile, "https://") == 0 {
		if err := GetCacheHttpFile(publicKeyFile, "/tmp/public.pem"); err != nil {
			log.Panicf("Could not load public key file via http: %s\n", err.Error())
		}
		publicKeyFile = "/tmp/public.pem"
	}
	publicKey, err := protocol.LoadPublicKey(privateKeyFile)
	if err != nil {
		log.Panicf("Could not load public key: %s\n", err.Error())
	}
	c.PublicKey = publicKey
}

func (c *Config) getEnv(key, defaultValue string) string {
	res := os.Getenv(key)
	if res == "" {
		return defaultValue
	}
	return res
}
