package main

import (
	"crypto/tls"
	"net"
	"sync/atomic"
	"time"

	"github.com/liudanking/yamux"

	log "github.com/liudanking/log4go"
)

type RemoteServer struct {
	laddr       *net.TCPAddr
	raddr       *net.TCPAddr
	streamCount int32
}

func NewRemoteServer(laddr, raddr string) (*RemoteServer, error) {
	_laddr, err := net.ResolveTCPAddr("tcp", laddr)
	if err != nil {
		return nil, err
	}
	_raddr, err := net.ResolveTCPAddr("tcp", raddr)
	if err != nil {
		return nil, err
	}

	rs := &RemoteServer{
		laddr:       _laddr,
		raddr:       _raddr,
		streamCount: 0,
	}
	return rs, nil
}

func (rs *RemoteServer) Serve(certFile, keyFile string) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		log.Error("load cert/key error:%v", err)
		return
	}
	config := tls.Config{Certificates: []tls.Certificate{cert}}
	// TODO: config
	l, err := tls.Listen("tcp", rs.laddr.String(), &config)
	if err != nil {
		log.Error("net.ListenTCP(%s) error:%v", rs.laddr.String(), err)
		return
	}

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Error("listenner accept connection error:%v", err)
			continue
		}
		log.Info("accept a tunnel connection:%s", conn.RemoteAddr().String())
		// TODO: set config
		session, err := yamux.Server(conn, nil)
		if err != nil {
			log.Error("create yamux server error:%v", err)
			continue
		}
		go rs.handleTunnel(session)
	}
}

func (rs *RemoteServer) handleTunnel(tunnel *yamux.Session) {
	for {
		stream, err := tunnel.AcceptStream()
		if err != nil {
			tunnel.Close()
			log.Error("accept stream error:%v", err)
			return
		}
		go rs.serveStream(stream)
	}
}

func (rs *RemoteServer) serveStream(stream *yamux.Stream) {
	defer stream.Close()
	start := time.Now()
	conn, err := net.DialTCP("tcp", nil, rs.raddr)
	if err != nil {
		log.Error("connect to remote error:%v", err)
		return
	}
	defer conn.Close()
	log.Debug("stream #%d data transfer", stream.StreamID())

	atomic.AddInt32(&rs.streamCount, 1)
	streamID := stream.StreamID()
	readChan := make(chan int64, 1)
	writeChan := make(chan int64, 1)
	var readBytes int64
	var writeBytes int64
	go pipe(stream, conn, readChan)
	go pipe(conn, stream, writeChan)
	for i := 0; i < 2; i++ {
		select {
		case readBytes = <-readChan:
			stream.Close()
			log.Debug("[#%d] read %d bytes", streamID, readBytes)
		case writeBytes = <-writeChan:
			// DON'T call conn.Close, it will trigger an error in pipe.
			// Just close stream, let the conn copy finished normally
			// conn.Close()
			log.Debug("[#%d] write %d bytes", streamID, writeBytes)
		}
	}
	log.Info("[#%d] r:%d w:%d t:%v c:%d",
		streamID, readBytes, writeBytes, time.Now().Sub(start), atomic.LoadInt32(&rs.streamCount))
	atomic.AddInt32(&rs.streamCount, -1)
}
