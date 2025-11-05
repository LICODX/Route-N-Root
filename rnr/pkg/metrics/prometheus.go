package metrics

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

type PrometheusMetrics struct {
	counters map[string]*Counter
	gauges   map[string]*Gauge
	histograms map[string]*Histogram
	mu       sync.RWMutex
}

type Counter struct {
	name  string
	help  string
	value int64
	mu    sync.Mutex
}

type Gauge struct {
	name  string
	help  string
	value float64
	mu    sync.Mutex
}

type Histogram struct {
	name   string
	help   string
	sum    float64
	count  int64
	buckets map[float64]int64
	mu     sync.Mutex
}

func NewPrometheusMetrics() *PrometheusMetrics {
	return &PrometheusMetrics{
		counters:   make(map[string]*Counter),
		gauges:     make(map[string]*Gauge),
		histograms: make(map[string]*Histogram),
	}
}

func (pm *PrometheusMetrics) RegisterCounter(name, help string) *Counter {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	
	if counter, exists := pm.counters[name]; exists {
		return counter
	}
	
	counter := &Counter{
		name: name,
		help: help,
	}
	pm.counters[name] = counter
	return counter
}

func (c *Counter) Inc() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value++
}

func (c *Counter) Add(delta int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value += delta
}

func (c *Counter) Get() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

func (pm *PrometheusMetrics) RegisterGauge(name, help string) *Gauge {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	
	if gauge, exists := pm.gauges[name]; exists {
		return gauge
	}
	
	gauge := &Gauge{
		name: name,
		help: help,
	}
	pm.gauges[name] = gauge
	return gauge
}

func (g *Gauge) Set(value float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.value = value
}

func (g *Gauge) Inc() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.value++
}

func (g *Gauge) Dec() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.value--
}

func (g *Gauge) Get() float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.value
}

func (pm *PrometheusMetrics) RegisterHistogram(name, help string, buckets []float64) *Histogram {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	
	if histogram, exists := pm.histograms[name]; exists {
		return histogram
	}
	
	bucketMap := make(map[float64]int64)
	for _, bucket := range buckets {
		bucketMap[bucket] = 0
	}
	
	histogram := &Histogram{
		name:    name,
		help:    help,
		buckets: bucketMap,
	}
	pm.histograms[name] = histogram
	return histogram
}

func (h *Histogram) Observe(value float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	h.sum += value
	h.count++
	
	for bucket := range h.buckets {
		if value <= bucket {
			h.buckets[bucket]++
		}
	}
}

func (h *Histogram) ObserveDuration(start time.Time) {
	duration := time.Since(start).Seconds()
	h.Observe(duration)
}

func (pm *PrometheusMetrics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pm.mu.RLock()
		defer pm.mu.RUnlock()
		
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		
		for _, counter := range pm.counters {
			fmt.Fprintf(w, "# HELP %s %s\n", counter.name, counter.help)
			fmt.Fprintf(w, "# TYPE %s counter\n", counter.name)
			fmt.Fprintf(w, "%s %d\n", counter.name, counter.Get())
		}
		
		for _, gauge := range pm.gauges {
			fmt.Fprintf(w, "# HELP %s %s\n", gauge.name, gauge.help)
			fmt.Fprintf(w, "# TYPE %s gauge\n", gauge.name)
			fmt.Fprintf(w, "%s %f\n", gauge.name, gauge.Get())
		}
		
		for _, histogram := range pm.histograms {
			histogram.mu.Lock()
			fmt.Fprintf(w, "# HELP %s %s\n", histogram.name, histogram.help)
			fmt.Fprintf(w, "# TYPE %s histogram\n", histogram.name)
			
			for bucket, count := range histogram.buckets {
				fmt.Fprintf(w, "%s_bucket{le=\"%f\"} %d\n", histogram.name, bucket, count)
			}
			fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", histogram.name, histogram.count)
			fmt.Fprintf(w, "%s_sum %f\n", histogram.name, histogram.sum)
			fmt.Fprintf(w, "%s_count %d\n", histogram.name, histogram.count)
			histogram.mu.Unlock()
		}
	}
}

type BlockchainMetrics struct {
	BlockHeight          *Gauge
	TotalTransactions    *Counter
	BlockProductionTime  *Histogram
	ValidatorCount       *Gauge
	PeerCount            *Gauge
	MempoolSize          *Gauge
	SyncStatus           *Gauge
	FinalizedBlocks      *Counter
	SlashedValidators    *Counter
	NetworkPartitions    *Counter
}

func NewBlockchainMetrics(pm *PrometheusMetrics) *BlockchainMetrics {
	return &BlockchainMetrics{
		BlockHeight: pm.RegisterGauge(
			"rnr_block_height",
			"Current blockchain height",
		),
		TotalTransactions: pm.RegisterCounter(
			"rnr_transactions_total",
			"Total number of processed transactions",
		),
		BlockProductionTime: pm.RegisterHistogram(
			"rnr_block_production_seconds",
			"Time taken to produce a block",
			[]float64{0.1, 0.5, 1.0, 2.0, 5.0, 10.0, 30.0},
		),
		ValidatorCount: pm.RegisterGauge(
			"rnr_validators_active",
			"Number of active validators",
		),
		PeerCount: pm.RegisterGauge(
			"rnr_peers_connected",
			"Number of connected peers",
		),
		MempoolSize: pm.RegisterGauge(
			"rnr_mempool_size",
			"Number of transactions in mempool",
		),
		SyncStatus: pm.RegisterGauge(
			"rnr_sync_status",
			"Sync status (1=synced, 0=syncing)",
		),
		FinalizedBlocks: pm.RegisterCounter(
			"rnr_blocks_finalized_total",
			"Total number of finalized blocks",
		),
		SlashedValidators: pm.RegisterCounter(
			"rnr_validators_slashed_total",
			"Total number of slashed validators",
		),
		NetworkPartitions: pm.RegisterCounter(
			"rnr_network_partitions_total",
			"Total number of network partition events detected",
		),
	}
}
