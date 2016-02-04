package main

import (
	"io"
	"net"
	"sync/atomic"
	"time"

	"github.com/hashicorp/yamux"

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

func (rs *RemoteServer) Serve() {
	l, err := net.ListenTCP("tcp", rs.laddr)
	if err != nil {
		log.Error("net.ListenTCP(%s) error:%v", rs.laddr.String(), err)
		return
	}

	for {
		conn, err := l.AcceptTCP()
		if err != nil {
			log.Error("listenner accept connection error:%v", err)
			continue
		}
		log.Debug("accept a connection:%s", conn.RemoteAddr().String())
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
			log.Error("accept stream error:%v", err)
			return
		}
		go rs.serveStream(stream)
	}
}

func (rs *RemoteServer) serveStream(stream *yamux.Stream) {
	start := time.Now()
	conn, err := net.DialTCP("tcp", nil, rs.raddr)
	if err != nil {
		log.Error("connect to remote error:%v", err)
		return
	}
	log.Debug("data transfer")

	atomic.AddInt32(&rs.streamCount, 1)
	streamID := stream.StreamID()
	writeChan := make(chan int64)
	go func() {
		n, err := io.Copy(conn, stream)
		if err != nil {
			log.Warn("copy conn to stream #%d error:%v", streamID, err)
		}
		conn.CloseRead()
		writeChan <- n
	}()

	n, err := io.Copy(stream, conn)
	if err != nil {
		log.Warn("copy stream #%d to conn error:%v", streamID, err)
	}
	writeBytes := <-writeChan
	conn.CloseWrite()
	stream.Close()
	log.Info("stream #%d r:%d w:%d ct:%v c:%d",
		streamID, n, writeBytes, time.Now().Sub(start), atomic.LoadInt32(&rs.streamCount))
	atomic.AddInt32(&rs.streamCount, -1)
}
