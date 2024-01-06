package pkg

import "github.com/prometheus/client_golang/prometheus"

var (
	EventServerClientsGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "events_server_clients",
		Help: "A gauge of events server clients.",
	})

	EventServerSessionsGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "events_server_sessions",
		Help: "A gauge of events server sessions.",
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
		EventServerSessionsGauge,
		EventServerInFlightGauge,
		EventServerRequestsCounter,
	)
}
