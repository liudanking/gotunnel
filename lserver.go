package main

import (
	"io"
	"net"
	"sync/atomic"
	"time"

	"github.com/hashicorp/yamux"

	log "github.com/liudanking/log4go"
)

const (
	S_START = 0x01
	S_TRANS = 0x02
	S_HEART = 0Xfe
	S_STOP  = 0Xff
)

type LocalServer struct {
	laddr *net.TCPAddr
	raddr *net.TCPAddr
	// remote connection, only used in local mode
	tunnel      *yamux.Session
	streamCount int32
}

func NewLocalServer(laddr, raddr string) (*LocalServer, error) {
	_laddr, err := net.ResolveTCPAddr("tcp", laddr)
	if err != nil {
		return nil, err
	}
	_raddr, err := net.ResolveTCPAddr("tcp", raddr)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTCP("tcp", nil, _raddr)
	if err != nil {
		log.Error("dial remote [%s] error:%v", _raddr.String(), err)
		return nil, err
	}
	// TODO: config client
	session, err := yamux.Client(conn, nil)
	if err != nil {
		log.Error("create yamux client error:%v", err)
		return nil, err
	}

	ls := &LocalServer{
		laddr:       _laddr,
		raddr:       _raddr,
		tunnel:      session,
		streamCount: 0,
	}
	return ls, nil
}

func (ls *LocalServer) Serve() {
	l, err := net.ListenTCP("tcp", ls.laddr)
	if err != nil {
		log.Error("listen [%s] error:%v", ls.laddr.String(), err)
		return
	}

	for {
		c, err := l.AcceptTCP()
		if err != nil {
			log.Error("accept connection error:%v", err)
			continue
		}
		go ls.transport(c)
	}
}

func (ls *LocalServer) transport(conn *net.TCPConn) {
	start := time.Now()
	stream, err := ls.tunnel.OpenStream()
	if err != nil {
		log.Error("open stream for %s error:%v", conn.RemoteAddr().String(), err)
		conn.Close()
		return
	}
	atomic.AddInt32(&ls.streamCount, 1)
	streamID := stream.StreamID()
	writeChan := make(chan int64)
	go func() {
		n, err := io.Copy(stream, conn)
		if err != nil {
			log.Warn("copy conn to stream #%d error:%v", streamID, err)
		}
		conn.CloseRead()
		writeChan <- n
	}()

	n, err := io.Copy(conn, stream)
	if err != nil {
		log.Warn("copy stream #%d to conn error:%v", streamID, err)
	}
	writeBytes := <-writeChan
	stream.Close()
	conn.CloseWrite()
	log.Info("stream #%d r:%d w:%d ct:%v c:%d",
		streamID, n, writeBytes, time.Now().Sub(start), atomic.LoadInt32(&ls.streamCount))
	atomic.AddInt32(&ls.streamCount, -1)
}
