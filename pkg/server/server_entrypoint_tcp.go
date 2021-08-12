package server

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"time"

	logger "github.com/sirupsen/logrus"
)

// TCPConnsChannels keeps track of the channels each TCP connection is interested in.
type TCPConnsChannels map[net.Conn][]string

// ChannelsTCPConns keeps track of the TCP connections interested in each channel.
type ChannelsTCPConns map[string][]net.Conn

// TCPEntryPoint is the TCP server.
type TCPEntryPoint struct {
	listener net.Listener

	connsChannels TCPConnsChannels
	channelsConns ChannelsTCPConns
}

type TCPListenerConfiguration struct {
	Address string
}

type tcpKeepAliveListener struct {
	*net.TCPListener
}

const (
	TCPSubscribe   = "SUBSCRIBE"
	TCPUnsubscribe = "UNSUBSCRIBE"
	TCPPublish     = "PUBLISH"
)

// NewTCPEntrypoint returns an initialized TCPEntryPoint.
func NewTCPEntrypoint(lnConfiguration *TCPListenerConfiguration) (*TCPEntryPoint, error) {
	listener, err := buildListener(lnConfiguration)
	if err != nil {
		return nil, fmt.Errorf("error initializing TCP entrypoint: %w", err)
	}

	return &TCPEntryPoint{
		listener:      listener,
		connsChannels: make(TCPConnsChannels),
		channelsConns: make(ChannelsTCPConns),
	}, nil
}

// TCPFanout is a message which is sent to a set of TCP connections.
type TCPFanout struct {
	channel string
	data    string
	conns   []net.Conn
}

// tcpFanout sends a message to a set of TCP connections.
func tcpFanout(f TCPFanout) {
	message := fmt.Sprintf("MSG %s %s", f.channel, f.data)
	for _, conn := range f.conns {
		conn.Write([]byte(message))
	}
}

// buildListener builds a keepalive TCP listener.
func buildListener(configuration *TCPListenerConfiguration) (net.Listener, error) {
	logger.Debugf("Open TCP listener at %s", configuration.Address)

	listener, err := net.Listen("tcp", configuration.Address)
	if err != nil {
		return nil, fmt.Errorf("error opening listener: %w", err)
	}

	listener = tcpKeepAliveListener{listener.(*net.TCPListener)}
	return listener, nil
}

// Accept accepts a TCP connection with keepalive options.
func (ln tcpKeepAliveListener) Accept() (net.Conn, error) {
	conn, err := ln.AcceptTCP()
	if err != nil {
		return nil, err
	}

	if err := conn.SetKeepAlive(true); err != nil {
		return nil, err
	}

	if err := conn.SetKeepAlivePeriod(3 * time.Minute); err != nil {
		// Some systems, such as OpenBSD, have no user-settable per-socket TCP
		// keepalive options.
		if !errors.Is(err, syscall.ENOPROTOOPT) {
			return nil, err
		}
	}

	return conn, nil
}

// Start starts the TCP server.
func (e *TCPEntryPoint) Start() {
	logger.Debug("Start TCP Server")

	for {
		conn, err := e.listener.Accept()
		if err != nil {
			logger.Error(err)

			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Temporary() {
				continue
			}

			return
		}

		go handleTCPConn(conn, e.connsChannels, e.channelsConns)
	}
}

// handleTCPConn handles a TCP connection.
func handleTCPConn(conn net.Conn, connsChannels TCPConnsChannels, channelsConns ChannelsTCPConns) {
	defer untrackTCPConn(conn, connsChannels, channelsConns)
	connsChannels[conn] = make([]string, 0)

	reader := bufio.NewReader(conn)
	for {
		data, err := reader.ReadBytes('\n')
		if err != nil {
			logger.Error(err)
			return
		}

		message := string(data)
		handleTCPMessage(message, conn, connsChannels, channelsConns)
	}
}

// handleTCPMessage handles a message sent over a TCP connection.
func handleTCPMessage(message string, conn net.Conn, connsChannels TCPConnsChannels, channelsConns ChannelsTCPConns) {
	parts := strings.Split(message, " ")
	switch kind := parts[0]; kind {
	case TCPSubscribe:
		handleTCPSubscribe(conn, parts[1:], connsChannels, channelsConns)
	case TCPUnsubscribe:
		handleTCPUnsubscribe(conn, parts[1:], connsChannels, channelsConns)
	case TCPPublish:
		handleTCPPublish(conn, parts[1:], channelsConns)
	}
}

// handleTCPSubscribe handles TCP messages of the kind TCPSubscribe.
// Messages of this kind should have the format SUBSCRIBE <channel>
// where channel is the name of the channel to subscribe to.
func handleTCPSubscribe(conn net.Conn, args []string, connsChannels TCPConnsChannels, channelsConns ChannelsTCPConns) {
	if len(args) != 1 {
		return
	}

	channel := args[0]
	okResp := fmt.Sprintf("OK %s %s", TCPSubscribe, channel)

	if tcIndex := indexOfTCPConn(channelsConns[channel], conn); tcIndex != -1 {
		conn.Write([]byte(okResp))
		return
	}

	connsChannels[conn] = append(connsChannels[conn], channel)
	channelsConns[channel] = append(channelsConns[channel], conn)
	conn.Write([]byte(okResp))
}

// handleTCPUnsubscribe handles TCP messages of the kind TCPUnsubscribe.
// Messages of this kind should have the format UNSUBSCRIBE <channel>
// where channel is the name of the channel to subscribe to.
func handleTCPUnsubscribe(conn net.Conn, args []string, connsChannels TCPConnsChannels, channelsConns ChannelsTCPConns) {
	if len(args) != 1 {
		return
	}

	channel := args[0]
	okResp := fmt.Sprintf("OK %s %s", TCPUnsubscribe, channel)

	if tcIndex := indexOfTCPConn(channelsConns[channel], conn); tcIndex == -1 {
		conn.Write([]byte(okResp))
		return
	}

	connsChannels[conn] = removeTCPChannel(connsChannels[conn], channel)
	channelsConns[channel] = removeTCPConn(channelsConns[channel], conn)
	conn.Write([]byte(okResp))
}

// handleTCPPublish handles TCP messages of the kind TCPPublish.
// Messages of this kind should have the format PUBLISH <channel> <data>
// where channel is the name of the channel to subscribe to and data is
// the data to publish.
func handleTCPPublish(conn net.Conn, args []string, channelsConns ChannelsTCPConns) {
	if len(args) != 2 {
		return
	}

	channel, data := args[0], args[1]
	if conns, ok := channelsConns[channel]; ok {
		go tcpFanout(TCPFanout{
			channel: channel,
			data:    data,
			conns:   conns,
		})
	}
}

// untrackTCPConn stops tracking a TCP connection.
func untrackTCPConn(conn net.Conn, connsChannels TCPConnsChannels, channelsConns ChannelsTCPConns) {
	for _, channel := range connsChannels[conn] {
		if channelTcs, ok := channelsConns[channel]; ok {
			channelsConns[channel] = removeTCPConn(channelTcs, conn)
			if len(channelsConns[channel]) == 0 {
				delete(channelsConns, channel)
			}
		}
	}

	delete(connsChannels, conn)
	err := conn.Close()
	if err != nil {
		logger.Error(err)
	}
}

// indexOfTCPChannel returns the index of a channel.
func indexOfTCPChannel(channels []string, channel string) int {
	for i, v := range channels {
		if v == channel {
			return i
		}
	}

	return -1
}

// indexOfTCPConn returns the index of a TCP connection.
func indexOfTCPConn(conns []net.Conn, conn net.Conn) int {
	for i, v := range conns {
		if v == conn {
			return i
		}
	}

	return -1
}

// removeTCPChannel removes a channel from an array of channels.
func removeTCPChannel(channels []string, channel string) []string {
	channelIndex := indexOfTCPChannel(channels, channel)

	if channelIndex == -1 {
		return channels
	}

	channels[channelIndex] = channels[len(channels)-1]
	return channels[:len(channels)-1]
}

// removeTCPConn removes a TCP connection from an array of TCP connections.
func removeTCPConn(conns []net.Conn, conn net.Conn) []net.Conn {
	tcIndex := indexOfTCPConn(conns, conn)

	if tcIndex == -1 {
		return conns
	}

	conns[tcIndex] = conns[len(conns)-1]
	return conns[:len(conns)-1]
}
