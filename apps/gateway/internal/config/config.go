package config

import (
	"os"
)

type Config struct {
	Host    string
	Port    string
	DataDir string
}

func Load() Config {
	host := os.Getenv("COPAW_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("COPAW_PORT")
	if port == "" {
		port = "8088"
	}
	dataDir := os.Getenv("COPAW_DATA_DIR")
	if dataDir == "" {
		dataDir = ".data"
	}
	return Config{Host: host, Port: port, DataDir: dataDir}
}
