package pubsub

import (
	"os"
	"os/signal"
	"syscall"
)

// Server is an instance of the pub/sub engine.
type Server struct {
	tcpEntryPoint *TCPEntryPoint
	signals       chan os.Signal
}

// NewServer returns an initialized Server.
func NewServer(entryPoint *TCPEntryPoint) *Server {
	srv := &Server{
		tcpEntryPoint: entryPoint,
		signals:       make(chan os.Signal, 1),
	}

	srv.configureSignals()

	return srv
}

// configureSignals configures signals to notify the signals channel
// when a SIGTERM is received.
func (s *Server) configureSignals() {
	signal.Notify(s.signals, syscall.SIGTERM)
}

// listenSignals blocks until a SIGTERM signal is received.
func (s *Server) listenSignals() {
	<-s.signals
}

// Start starts the Server.
func (s *Server) Start() {
	go s.tcpEntryPoint.Start()
	s.listenSignals()
}
