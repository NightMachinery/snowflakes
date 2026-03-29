package main

import (
	"log"
	"net/http"
	"snowflakes/internal/snowflakes"
)

func main() {
	cfg := snowflakes.ConfigFromEnv()
	app, err := snowflakes.NewApp(cfg)
	if err != nil {
		log.Fatal(err)
	}

	addr := cfg.BindAddr()
	log.Printf("snowflakes listening on %s", addr)
	if err := http.ListenAndServe(addr, app.Handler()); err != nil {
		log.Fatal(err)
	}
}
