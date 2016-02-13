package main

import (
	"crypto/tls"
	"net"
	"sync/atomic"
	"time"

	"github.com/hashicorp/yamux"

	log "github.com/liudanking/log4go"
)

type RemoteServer struct {
	laddr       string
	raddr       string
	ltls        bool
	rtls        bool
	streamCount int32
}

func NewRemoteServer(laddr, raddr string, ltls, rtls bool) (*RemoteServer, error) {
	rs := &RemoteServer{
		laddr:       laddr,
		raddr:       raddr,
		ltls:        ltls,
		rtls:        rtls,
		streamCount: 0,
	}
	return rs, nil
}

func (rs *RemoteServer) Serve(certFile, keyFile string) {
	var l net.Listener
	var err error
	if rs.ltls {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			log.Error("load cert/key error:%v", err)
			return
		}
		config := tls.Config{Certificates: []tls.Certificate{cert}}
		l, err = tls.Listen("tcp", rs.laddr, &config)
	} else {
		l, err = net.Listen("tcp", rs.laddr)
	}
	if err != nil {
		log.Error("net.ListenTCP(%s) error:%v", rs.laddr, err)
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
	var conn net.Conn
	var err error
	if rs.rtls {
		config := tls.Config{
			InsecureSkipVerify: DEBUG,
			ClientSessionCache: TLS_SESSION_CACHE,
		}
		conn, err = tls.Dial("tcp", rs.raddr, &config)
	} else {
		conn, err = net.Dial("tcp", rs.raddr)
	}
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
