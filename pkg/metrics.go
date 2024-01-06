package pkg

import "github.com/prometheus/client_golang/prometheus"

var (
	EventServerClientsGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "events_server_clients",
		Help: "A gauge of clients connected to the events server.",
	})

	EventServerInFlightGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "events_server_in_flight_requests",
		Help: "A gauge of requests being handled by the events server.",
	})

	EventServerRequestsCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "events_server_requests_total",
		Help: "A counter for requests to the events server.",
	}, []string{"code", "method"})
)

func init() {
	prometheus.MustRegister(
		EventServerClientsGauge,
		EventServerInFlightGauge,
		EventServerRequestsCounter,
	)
}
