package main

import (
	"fmt"
	"log"
	"net/http"

	"nextai/apps/gateway/internal/app"
	"nextai/apps/gateway/internal/config"
)

func main() {
	if path, loaded, err := loadEnvFile(); err != nil {
		log.Printf("load env file failed: path=%s err=%v", path, err)
	} else if loaded > 0 {
		log.Printf("loaded %d env values from %s", loaded, path)
	}

	cfg := config.Load()
	srv, err := app.NewServer(cfg)
	if err != nil {
		log.Fatalf("init server failed: %v", err)
	}
	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)
	log.Printf("gateway listening on %s", addr)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatalf("listen failed: %v", err)
	}
}
