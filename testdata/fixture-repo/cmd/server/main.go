package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/example/fixture/internal/config"
	"github.com/example/fixture/src/handlers"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	if err := config.Validate(cfg); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	_ = mux
	_ = handlers.NewPaymentHandler(nil)

	addr := fmt.Sprintf(":%d", cfg.HTTPPort)
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
