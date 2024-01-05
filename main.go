package main

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mtaylor91/event-server/pkg"
	log "github.com/sirupsen/logrus"
)

func main() {
	manager := pkg.NewManager()
	router := mux.NewRouter()
	router.HandleFunc("/api/v1/health", manager.HealthHandler)
	router.HandleFunc("/api/v1/socket", manager.SocketHandler)
	fmt.Println("Starting server on port 8080...")
	err := http.ListenAndServe(":8080", router)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
