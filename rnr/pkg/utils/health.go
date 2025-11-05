package utils

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

type HealthStatus string

const (
	StatusHealthy   HealthStatus = "healthy"
	StatusDegraded  HealthStatus = "degraded"
	StatusUnhealthy HealthStatus = "unhealthy"
)

type ComponentHealth struct {
	Name      string       `json:"name"`
	Status    HealthStatus `json:"status"`
	Message   string       `json:"message"`
	LastCheck time.Time    `json:"last_check"`
	Uptime    time.Duration `json:"uptime"`
}

type HealthMonitor struct {
	components     map[string]*ComponentHealth
	mutex          sync.RWMutex
	startTime      time.Time
	checkInterval  time.Duration
	healthChecks   map[string]func() (HealthStatus, string)
}

func NewHealthMonitor(checkInterval time.Duration) *HealthMonitor {
	return &HealthMonitor{
		components:    make(map[string]*ComponentHealth),
		startTime:     time.Now(),
		checkInterval: checkInterval,
		healthChecks:  make(map[string]func() (HealthStatus, string)),
	}
}

func (hm *HealthMonitor) RegisterComponent(name string, healthCheck func() (HealthStatus, string)) {
	hm.mutex.Lock()
	defer hm.mutex.Unlock()
	
	hm.components[name] = &ComponentHealth{
		Name:      name,
		Status:    StatusHealthy,
		LastCheck: time.Now(),
		Uptime:    0,
	}
	
	hm.healthChecks[name] = healthCheck
	log.Printf("💚 Health monitor registered: %s", name)
}

func (hm *HealthMonitor) CheckHealth(name string) {
	hm.mutex.Lock()
	defer hm.mutex.Unlock()
	
	if check, exists := hm.healthChecks[name]; exists {
		status, message := check()
		
		if comp, ok := hm.components[name]; ok {
			comp.Status = status
			comp.Message = message
			comp.LastCheck = time.Now()
			comp.Uptime = time.Since(hm.startTime)
			
			if status == StatusUnhealthy {
				log.Printf("⚠️  Component %s is UNHEALTHY: %s", name, message)
			} else if status == StatusDegraded {
				log.Printf("⚠️  Component %s is DEGRADED: %s", name, message)
			}
		}
	}
}

func (hm *HealthMonitor) CheckAllHealth() {
	for name := range hm.healthChecks {
		hm.CheckHealth(name)
	}
}

func (hm *HealthMonitor) GetHealth(name string) *ComponentHealth {
	hm.mutex.RLock()
	defer hm.mutex.RUnlock()
	
	if comp, exists := hm.components[name]; exists {
		return comp
	}
	return nil
}

func (hm *HealthMonitor) GetOverallHealth() HealthStatus {
	hm.mutex.RLock()
	defer hm.mutex.RUnlock()
	
	hasUnhealthy := false
	hasDegraded := false
	
	for _, comp := range hm.components {
		if comp.Status == StatusUnhealthy {
			hasUnhealthy = true
		} else if comp.Status == StatusDegraded {
			hasDegraded = true
		}
	}
	
	if hasUnhealthy {
		return StatusUnhealthy
	}
	if hasDegraded {
		return StatusDegraded
	}
	return StatusHealthy
}

func (hm *HealthMonitor) GetHealthReport() string {
	hm.mutex.RLock()
	defer hm.mutex.RUnlock()
	
	report := map[string]interface{}{
		"overall_status": hm.GetOverallHealth(),
		"uptime":         time.Since(hm.startTime).String(),
		"components":     hm.components,
		"timestamp":      time.Now(),
	}
	
	jsonReport, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error generating report: %v", err)
	}
	
	return string(jsonReport)
}

func (hm *HealthMonitor) StartPeriodicChecks() {
	go func() {
		ticker := time.NewTicker(hm.checkInterval)
		defer ticker.Stop()
		
		for range ticker.C {
			hm.CheckAllHealth()
		}
	}()
	
	log.Printf("💚 Health monitor started (interval: %v)", hm.checkInterval)
}

type MetricsCollector struct {
	metrics map[string]interface{}
	mutex   sync.RWMutex
}

func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		metrics: make(map[string]interface{}),
	}
}

func (mc *MetricsCollector) RecordMetric(name string, value interface{}) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()
	mc.metrics[name] = value
}

func (mc *MetricsCollector) GetMetric(name string) interface{} {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()
	return mc.metrics[name]
}

func (mc *MetricsCollector) GetAllMetrics() map[string]interface{} {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()
	
	metrics := make(map[string]interface{})
	for k, v := range mc.metrics {
		metrics[k] = v
	}
	return metrics
}

func (mc *MetricsCollector) IncrementCounter(name string) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()
	
	if val, exists := mc.metrics[name]; exists {
		if counter, ok := val.(int64); ok {
			mc.metrics[name] = counter + 1
			return
		}
	}
	mc.metrics[name] = int64(1)
}

func (mc *MetricsCollector) RecordDuration(name string, duration time.Duration) {
	mc.RecordMetric(name, duration.Milliseconds())
}
