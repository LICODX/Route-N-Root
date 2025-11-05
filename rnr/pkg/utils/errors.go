package utils

import (
	"fmt"
	"log"
	"runtime/debug"
	"time"
)

type ErrorSeverity int

const (
	SeverityLow ErrorSeverity = iota
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

type RecoverableError struct {
	Err       error
	Severity  ErrorSeverity
	Component string
	Timestamp time.Time
	Retryable bool
}

func NewRecoverableError(err error, severity ErrorSeverity, component string, retryable bool) *RecoverableError {
	return &RecoverableError{
		Err:       err,
		Severity:  severity,
		Component: component,
		Timestamp: time.Now(),
		Retryable: retryable,
	}
}

func (e *RecoverableError) Error() string {
	return fmt.Sprintf("[%s] %s: %v", e.Component, e.SeverityString(), e.Err)
}

func (e *RecoverableError) SeverityString() string {
	switch e.Severity {
	case SeverityLow:
		return "LOW"
	case SeverityMedium:
		return "MEDIUM"
	case SeverityHigh:
		return "HIGH"
	case SeverityCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

type ErrorRecovery struct {
	maxRetries    int
	retryDelay    time.Duration
	errorHandlers map[string]func(error) error
}

func NewErrorRecovery(maxRetries int, retryDelay time.Duration) *ErrorRecovery {
	return &ErrorRecovery{
		maxRetries:    maxRetries,
		retryDelay:    retryDelay,
		errorHandlers: make(map[string]func(error) error),
	}
}

func (er *ErrorRecovery) RegisterHandler(component string, handler func(error) error) {
	er.errorHandlers[component] = handler
}

func (er *ErrorRecovery) RetryWithBackoff(operation func() error, component string) error {
	var lastErr error
	
	for attempt := 0; attempt <= er.maxRetries; attempt++ {
		if attempt > 0 {
			delay := er.retryDelay * time.Duration(1<<uint(attempt-1))
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
			log.Printf("⏳ Retry attempt %d/%d for %s (delay: %v)", attempt, er.maxRetries, component, delay)
			time.Sleep(delay)
		}
		
		err := operation()
		if err == nil {
			if attempt > 0 {
				log.Printf("✅ Recovery successful for %s after %d attempts", component, attempt)
			}
			return nil
		}
		
		lastErr = err
		
		if handler, exists := er.errorHandlers[component]; exists {
			if handlerErr := handler(err); handlerErr == nil {
				return nil
			}
		}
	}
	
	return fmt.Errorf("operation failed after %d retries: %w", er.maxRetries, lastErr)
}

func RecoverFromPanic(component string) {
	if r := recover(); r != nil {
		log.Printf("🚨 PANIC RECOVERED in %s: %v", component, r)
		log.Printf("Stack trace:\n%s", debug.Stack())
	}
}

func SafeGoroutine(component string, fn func()) {
	go func() {
		defer RecoverFromPanic(component)
		fn()
	}()
}

type CircuitBreaker struct {
	name           string
	maxFailures    int
	resetTimeout   time.Duration
	failures       int
	lastFailTime   time.Time
	state          string
	halfOpenMax    int
	halfOpenTries  int
}

func NewCircuitBreaker(name string, maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		name:          name,
		maxFailures:   maxFailures,
		resetTimeout:  resetTimeout,
		state:         "closed",
		halfOpenMax:   3,
	}
}

func (cb *CircuitBreaker) Call(operation func() error) error {
	if cb.state == "open" {
		if time.Since(cb.lastFailTime) > cb.resetTimeout {
			log.Printf("🔄 Circuit breaker %s: OPEN -> HALF-OPEN", cb.name)
			cb.state = "half-open"
			cb.halfOpenTries = 0
		} else {
			return fmt.Errorf("circuit breaker %s is OPEN", cb.name)
		}
	}
	
	err := operation()
	
	if err != nil {
		cb.failures++
		cb.lastFailTime = time.Now()
		
		if cb.state == "half-open" {
			log.Printf("⚠️  Circuit breaker %s: HALF-OPEN -> OPEN (failure during test)", cb.name)
			cb.state = "open"
			return fmt.Errorf("circuit breaker %s reopened: %w", cb.name, err)
		}
		
		if cb.failures >= cb.maxFailures {
			log.Printf("🔴 Circuit breaker %s: CLOSED -> OPEN (%d failures)", cb.name, cb.failures)
			cb.state = "open"
		}
		
		return err
	}
	
	if cb.state == "half-open" {
		cb.halfOpenTries++
		if cb.halfOpenTries >= cb.halfOpenMax {
			log.Printf("✅ Circuit breaker %s: HALF-OPEN -> CLOSED (recovery confirmed)", cb.name)
			cb.state = "closed"
			cb.failures = 0
		}
	} else if cb.state == "closed" {
		cb.failures = 0
	}
	
	return nil
}

func (cb *CircuitBreaker) GetState() string {
	return cb.state
}

func (cb *CircuitBreaker) Reset() {
	cb.state = "closed"
	cb.failures = 0
	cb.halfOpenTries = 0
	log.Printf("🔄 Circuit breaker %s manually reset", cb.name)
}
