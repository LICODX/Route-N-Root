package consensus

import (
        "fmt"
        "log"
        "net"
)

// PoBTestServer handles incoming PoB speed test connections
type PoBTestServer struct {
        port     int
        listener net.Listener
        done     chan struct{}
}

// NewPoBTestServer creates a new PoB test server
func NewPoBTestServer(port int) *PoBTestServer {
        return &PoBTestServer{
                port: port,
                done: make(chan struct{}),
        }
}

// Start begins listening for PoB test connections
func (s *PoBTestServer) Start() error {
        listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
        if err != nil {
                return fmt.Errorf("FATAL: failed to start PoB test server on port %d: %w", s.port, err)
        }
        
        s.listener = listener
        log.Printf("✅ PoB Test Server listening on port %d", s.port)
        
        go s.acceptConnections()
        return nil
}

// acceptConnections handles incoming PoB test connections
// Gracefully stops on shutdown signal or listener close
func (s *PoBTestServer) acceptConnections() {
        for {
                select {
                case <-s.done:
                        log.Printf("🛑 PoB test server stopped")
                        return
                default:
                }
                
                conn, err := s.listener.Accept()
                if err != nil {
                        // Check if listener was closed (expected during shutdown)
                        select {
                        case <-s.done:
                                return
                        default:
                                log.Printf("⚠️  PoB test server accept error: %v", err)
                                continue
                        }
                }
                
                // Handle connection in goroutine
                go func(c net.Conn) {
                        if err := HandlePoBTestRequest(c); err != nil {
                                log.Printf("⚠️  PoB test handler error: %v", err)
                        }
                }(conn)
        }
}

// Stop closes the PoB test server gracefully
func (s *PoBTestServer) Stop() error {
        close(s.done)
        if s.listener != nil {
                return s.listener.Close()
        }
        return nil
}
