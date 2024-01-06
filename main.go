package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/mtaylor91/event-server/pkg"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

func main() {
	manager := pkg.NewManager()

	eventsRouter := mux.NewRouter()
	eventsRouter.HandleFunc("/api/v1/health", manager.HealthHandler)
	eventsRouter.HandleFunc("/api/v1/socket", manager.SocketHandler)

	eventsServer := &http.Server{
		Addr: ":8080",
		Handler: promhttp.InstrumentHandlerInFlight(pkg.EventServerInFlightGauge,
			promhttp.InstrumentHandlerCounter(pkg.EventServerRequestsCounter,
				eventsRouter)),
	}

	metricsRouter := mux.NewRouter()
	metricsRouter.Handle("/metrics", promhttp.Handler())

	metricsServer := &http.Server{
		Addr:    ":8081",
		Handler: metricsRouter,
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	log.Info("Starting events server on port 8080...")
	go func() {
		err := eventsServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Fatal("Events server failed: ", err)
		}
	}()

	log.Info("Starting metrics server on port 8081...")
	go func() {
		err := metricsServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Fatal("Metrics server failed: ", err)
		}
	}()

	<-done

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Info("Shutting down events server...")
	if err := eventsServer.Shutdown(ctx); err != nil {
		log.Fatal("Events server shutdown failed: ", err)
	}

	log.Info("Shutting down metrics server...")
	if err := metricsServer.Shutdown(ctx); err != nil {
		log.Fatal("Metrics server shutdown failed: ", err)
	}
}
