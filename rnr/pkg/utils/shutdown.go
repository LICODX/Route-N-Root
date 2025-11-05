package utils

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type ShutdownManager struct {
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	shutdownHooks  []func() error
	hooksMutex     sync.Mutex
	gracePeriod    time.Duration
	shutdownSignal chan os.Signal
}

func NewShutdownManager(gracePeriod time.Duration) *ShutdownManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	sm := &ShutdownManager{
		ctx:            ctx,
		cancel:         cancel,
		shutdownHooks:  make([]func() error, 0),
		gracePeriod:    gracePeriod,
		shutdownSignal: make(chan os.Signal, 1),
	}
	
	signal.Notify(sm.shutdownSignal, syscall.SIGINT, syscall.SIGTERM)
	
	go sm.waitForShutdownSignal()
	
	return sm
}

func (sm *ShutdownManager) waitForShutdownSignal() {
	sig := <-sm.shutdownSignal
	log.Printf("🛑 Received shutdown signal: %v", sig)
	sm.InitiateShutdown()
}

func (sm *ShutdownManager) RegisterShutdownHook(name string, hook func() error) {
	sm.hooksMutex.Lock()
	defer sm.hooksMutex.Unlock()
	
	wrappedHook := func() error {
		log.Printf("📦 Executing shutdown hook: %s", name)
		err := hook()
		if err != nil {
			log.Printf("⚠️  Shutdown hook %s failed: %v", name, err)
			return err
		}
		log.Printf("✅ Shutdown hook %s completed", name)
		return nil
	}
	
	sm.shutdownHooks = append(sm.shutdownHooks, wrappedHook)
}

func (sm *ShutdownManager) InitiateShutdown() {
	log.Printf("🔄 Initiating graceful shutdown (grace period: %v)...", sm.gracePeriod)
	
	sm.cancel()
	
	done := make(chan struct{})
	go func() {
		sm.wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		log.Printf("✅ All goroutines completed gracefully")
	case <-time.After(sm.gracePeriod):
		log.Printf("⚠️  Grace period expired, forcing shutdown")
	}
	
	sm.executeShutdownHooks()
	
	log.Printf("👋 Shutdown complete")
	os.Exit(0)
}

func (sm *ShutdownManager) executeShutdownHooks() {
	sm.hooksMutex.Lock()
	hooks := make([]func() error, len(sm.shutdownHooks))
	copy(hooks, sm.shutdownHooks)
	sm.hooksMutex.Unlock()
	
	for i := len(hooks) - 1; i >= 0; i-- {
		if err := hooks[i](); err != nil {
			log.Printf("⚠️  Shutdown hook failed: %v", err)
		}
	}
}

func (sm *ShutdownManager) Context() context.Context {
	return sm.ctx
}

func (sm *ShutdownManager) AddTask() {
	sm.wg.Add(1)
}

func (sm *ShutdownManager) TaskDone() {
	sm.wg.Done()
}

func (sm *ShutdownManager) WaitGroup() *sync.WaitGroup {
	return &sm.wg
}

type ResourceLimiter struct {
	maxGoroutines int
	semaphore     chan struct{}
	active        int
	mutex         sync.Mutex
}

func NewResourceLimiter(maxGoroutines int) *ResourceLimiter {
	return &ResourceLimiter{
		maxGoroutines: maxGoroutines,
		semaphore:     make(chan struct{}, maxGoroutines),
	}
}

func (rl *ResourceLimiter) Acquire(timeout time.Duration) bool {
	select {
	case rl.semaphore <- struct{}{}:
		rl.mutex.Lock()
		rl.active++
		rl.mutex.Unlock()
		return true
	case <-time.After(timeout):
		return false
	}
}

func (rl *ResourceLimiter) Release() {
	<-rl.semaphore
	rl.mutex.Lock()
	rl.active--
	rl.mutex.Unlock()
}

func (rl *ResourceLimiter) GetActiveCount() int {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()
	return rl.active
}

func (rl *ResourceLimiter) GetCapacity() int {
	return rl.maxGoroutines
}

func (rl *ResourceLimiter) Execute(task func(), timeout time.Duration) bool {
	if !rl.Acquire(timeout) {
		return false
	}
	
	go func() {
		defer rl.Release()
		task()
	}()
	
	return true
}
