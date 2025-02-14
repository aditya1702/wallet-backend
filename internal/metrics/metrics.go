package metrics

import (
	"fmt"
	"strconv"

	"github.com/alitto/pond"
	"github.com/dlmiddlecote/sqlstats"
	"github.com/jmoiron/sqlx"
	"github.com/prometheus/client_golang/prometheus"
)

// MetricsService handles all metrics for the wallet-backend
type MetricsService struct {
	registry *prometheus.Registry
	db       *sqlx.DB

	// Ingest Service Metrics
	numPaymentOpsIngestedPerLedger      *prometheus.GaugeVec
	numTssTransactionsIngestedPerLedger *prometheus.GaugeVec
	latestLedgerIngested                prometheus.Gauge
	ingestionDuration                   *prometheus.SummaryVec

	// TSS Service Metrics
	numTSSTransactionsSubmitted      prometheus.Counter
	timeUntilTSSTransactionInclusion *prometheus.SummaryVec

	// Account Metrics
	activeAccounts prometheus.Gauge

	// RPC Service Metrics
	rpcRequestsTotal     *prometheus.CounterVec
	rpcRequestsDuration  *prometheus.SummaryVec
	rpcEndpointFailures  *prometheus.CounterVec
	rpcEndpointSuccesses *prometheus.CounterVec
	rpcServiceHealth     prometheus.Gauge
	rpcLatestLedger      prometheus.Gauge

	// TSS Transaction Status Metrics
	tssTransactionCurrentStates *prometheus.GaugeVec

	// HTTP Request Metrics
	numRequestsTotal *prometheus.CounterVec
	requestsDuration *prometheus.SummaryVec

	// DB Query Metrics
	dbQueryDuration *prometheus.SummaryVec
	dbQueriesTotal  *prometheus.CounterVec

	pools map[string]*pond.WorkerPool
}

// NewMetricsService creates a new metrics service with all metrics registered
func NewMetricsService(db *sqlx.DB) *MetricsService {
	m := &MetricsService{
		registry: prometheus.NewRegistry(),
		db:       db,
		pools:    make(map[string]*pond.WorkerPool),
	}

	// Ingest Service Metrics
	m.numPaymentOpsIngestedPerLedger = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "num_payment_ops_ingested_per_ledger",
			Help: "Number of payment operations ingested per ledger",
		},
		[]string{"operation_type"},
	)
	m.numTssTransactionsIngestedPerLedger = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "num_tss_transactions_ingested_per_ledger",
			Help: "Number of tss transactions ingested per ledger",
		},
		[]string{"status"},
	)
	m.latestLedgerIngested = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "latest_ledger_ingested",
			Help: "Latest ledger ingested",
		},
	)
	m.ingestionDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "ingestion_duration_seconds",
			Help:       "Duration of ledger ingestion",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"type"},
	)

	// TSS Service Metrics
	m.numTSSTransactionsSubmitted = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "num_tss_transactions_submitted",
			Help: "Total number of transactions submitted to TSS",
		},
	)
	m.timeUntilTSSTransactionInclusion = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "tss_transaction_inclusion_time_seconds",
			Help:       "Time from transaction submission to ledger inclusion (success or failure)",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"status"},
	)

	// Account Metrics
	m.activeAccounts = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "active_accounts",
			Help: "Number of currently registered active accounts",
		},
	)

	// RPC Service Metrics
	m.rpcRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rpc_requests_total",
			Help: "Total number of RPC requests",
		},
		[]string{"endpoint"},
	)
	m.rpcRequestsDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "rpc_requests_duration_seconds",
			Help:       "Duration of RPC requests in seconds",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"endpoint"},
	)
	m.rpcEndpointFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rpc_endpoint_failures_total",
			Help: "Total number of RPC endpoint failures",
		},
		[]string{"endpoint"},
	)
	m.rpcEndpointSuccesses = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rpc_endpoint_successes_total",
			Help: "Total number of successful RPC requests",
		},
		[]string{"endpoint"},
	)
	m.rpcServiceHealth = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "rpc_service_health",
			Help: "RPC service health status (1 for healthy, 0 for unhealthy)",
		},
	)
	m.rpcLatestLedger = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "rpc_latest_ledger",
			Help: "Latest ledger number reported by the RPC service",
		},
	)

	m.tssTransactionCurrentStates = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tss_transaction_current_states",
			Help: "Current number of TSS transactions in each state",
		},
		[]string{"channel", "status"},
	)

	// HTTP Request Metrics
	m.numRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"endpoint", "method", "status_code"},
	)
	m.requestsDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "http_request_duration_seconds",
			Help:       "Duration of HTTP requests in seconds",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"endpoint", "method"},
	)

	// DB Query Metrics
	m.dbQueryDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "db_query_duration_seconds",
			Help:       "Duration of database queries",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"query_type", "table"},
	)
	m.dbQueriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "db_queries_total",
			Help: "Total number of database queries",
		},
		[]string{"query_type", "table"},
	)
	m.registerMetrics()
	return m
}

func (m *MetricsService) registerMetrics() {
	collector := sqlstats.NewStatsCollector("wallet-backend-db", m.db)
	m.registry.MustRegister(
		collector,
		m.numPaymentOpsIngestedPerLedger,
		m.numTssTransactionsIngestedPerLedger,
		m.latestLedgerIngested,
		m.ingestionDuration,
		m.numTSSTransactionsSubmitted,
		m.timeUntilTSSTransactionInclusion,
		m.activeAccounts,
		m.rpcRequestsTotal,
		m.rpcRequestsDuration,
		m.rpcEndpointFailures,
		m.rpcEndpointSuccesses,
		m.rpcServiceHealth,
		m.rpcLatestLedger,
		m.tssTransactionCurrentStates,
		m.numRequestsTotal,
		m.requestsDuration,
		m.dbQueryDuration,
		m.dbQueriesTotal,
	)
}

// RegisterPool registers a worker pool for metrics collection
func (m *MetricsService) RegisterPoolMetrics(channel string, pool *pond.WorkerPool) {
	m.pools[channel] = pool

	m.registry.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name:        fmt.Sprintf("pool_workers_running_%s", channel),
			Help:        "Number of running worker goroutines",
			ConstLabels: prometheus.Labels{"channel": channel},
		},
		func() float64 {
			return float64(pool.RunningWorkers())
		},
	))

	m.registry.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name:        fmt.Sprintf("pool_workers_idle_%s", channel),
			Help:        "Number of idle worker goroutines",
			ConstLabels: prometheus.Labels{"channel": channel},
		},
		func() float64 {
			return float64(pool.IdleWorkers())
		},
	))

	m.registry.MustRegister(prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name:        fmt.Sprintf("pool_tasks_submitted_total_%s", channel),
			Help:        "Number of tasks submitted",
			ConstLabels: prometheus.Labels{"channel": channel},
		},
		func() float64 {
			return float64(pool.SubmittedTasks())
		},
	))

	m.registry.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name:        fmt.Sprintf("pool_tasks_waiting_%s", channel),
			Help:        "Number of tasks currently waiting in the queue",
			ConstLabels: prometheus.Labels{"channel": channel},
		},
		func() float64 {
			return float64(pool.WaitingTasks())
		},
	))

	m.registry.MustRegister(prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name:        fmt.Sprintf("pool_tasks_successful_total_%s", channel),
			Help:        "Number of tasks that completed successfully",
			ConstLabels: prometheus.Labels{"channel": channel},
		},
		func() float64 {
			return float64(pool.SuccessfulTasks())
		},
	))

	m.registry.MustRegister(prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name:        fmt.Sprintf("pool_tasks_failed_total_%s", channel),
			Help:        "Number of tasks that completed with panic",
			ConstLabels: prometheus.Labels{"channel": channel},
		},
		func() float64 {
			return float64(pool.FailedTasks())
		},
	))

	m.registry.MustRegister(prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name:        fmt.Sprintf("pool_tasks_completed_total_%s", channel),
			Help:        "Number of tasks that completed either successfully or with panic",
			ConstLabels: prometheus.Labels{"channel": channel},
		},
		func() float64 {
			return float64(pool.CompletedTasks())
		},
	))
}

// GetRegistry returns the prometheus registry
func (m *MetricsService) GetRegistry() *prometheus.Registry {
	return m.registry
}

// Ingest Service Metrics
func (m *MetricsService) SetNumPaymentOpsIngestedPerLedger(operationType string, value int) {
	m.numPaymentOpsIngestedPerLedger.WithLabelValues(operationType).Set(float64(value))
}

func (m *MetricsService) SetNumTssTransactionsIngestedPerLedger(status string, value float64) {
	m.numTssTransactionsIngestedPerLedger.WithLabelValues(status).Set(value)
}

func (m *MetricsService) SetLatestLedgerIngested(value float64) {
	m.latestLedgerIngested.Set(value)
}

func (m *MetricsService) ObserveIngestionDuration(ingestionType string, duration float64) {
	m.ingestionDuration.WithLabelValues(ingestionType).Observe(duration)
}

// TSS Service Metrics
func (m *MetricsService) IncNumTSSTransactionsSubmitted() {
	m.numTSSTransactionsSubmitted.Inc()
}

// ObserveTSSTransactionInclusionTime records the time taken for a transaction to be included in the ledger
func (m *MetricsService) ObserveTSSTransactionInclusionTime(status string, durationSeconds float64) {
	m.timeUntilTSSTransactionInclusion.WithLabelValues(status).Observe(durationSeconds)
}

// Account Service Metrics
func (m *MetricsService) IncActiveAccount() {
	m.activeAccounts.Inc()
}

func (m *MetricsService) DecActiveAccount() {
	m.activeAccounts.Dec()
}

// RPC Service Metrics
func (m *MetricsService) IncRPCRequests(endpoint string) {
	m.rpcRequestsTotal.WithLabelValues(endpoint).Inc()
}

func (m *MetricsService) ObserveRPCRequestDuration(endpoint string, duration float64) {
	m.rpcRequestsDuration.WithLabelValues(endpoint).Observe(duration)
}

func (m *MetricsService) IncRPCEndpointFailure(endpoint string) {
	m.rpcEndpointFailures.WithLabelValues(endpoint).Inc()
}

func (m *MetricsService) IncRPCEndpointSuccess(endpoint string) {
	m.rpcEndpointSuccesses.WithLabelValues(endpoint).Inc()
}

func (m *MetricsService) SetRPCServiceHealth(healthy bool) {
	if healthy {
		m.rpcServiceHealth.Set(1)
	} else {
		m.rpcServiceHealth.Set(0)
	}
}

func (m *MetricsService) SetRPCLatestLedger(ledger int64) {
	m.rpcLatestLedger.Set(float64(ledger))
}

// HTTP Request Metrics
func (m *MetricsService) IncNumRequests(endpoint, method string, statusCode int) {
	m.numRequestsTotal.WithLabelValues(endpoint, method, strconv.Itoa(statusCode)).Inc()
}

func (m *MetricsService) ObserveRequestDuration(endpoint, method string, duration float64) {
	m.requestsDuration.WithLabelValues(endpoint, method).Observe(duration)
}

// DB Query Metrics
func (m *MetricsService) ObserveDBQueryDuration(queryType, table string, duration float64) {
	m.dbQueryDuration.WithLabelValues(queryType, table).Observe(duration)
}

func (m *MetricsService) IncDBQuery(queryType, table string) {
	m.dbQueriesTotal.WithLabelValues(queryType, table).Inc()
}

// TSS Transaction Status Metrics
func (m *MetricsService) RecordTSSTransactionStatusTransition(channel, oldStatus, newStatus string) {
	if oldStatus != "" {
		m.tssTransactionCurrentStates.WithLabelValues(channel, oldStatus).Dec()
	}
	m.tssTransactionCurrentStates.WithLabelValues(channel, newStatus).Inc()
}
