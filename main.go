package main

import (
	"os"

	"github.com/FungiRealtime/pubsub/pkg/server"
	logger "github.com/sirupsen/logrus"
)

var (
	TCPPort = os.Getenv("TCP_PORT")
)

func main() {
	var address string
	if TCPPort == "" {
		address = ":4040"
	} else {
		address = ":" + TCPPort
	}

	entrypoint, err := server.NewTCPEntrypoint(&server.TCPListenerConfiguration{
		Address: address,
	})
	if err != nil {
		logger.Fatal(err)
	}

	srv := server.NewServer(entrypoint)
	srv.Start()
}
