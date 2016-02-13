package main

import (
	"crypto/tls"
	"math/rand"
	"net"
	"sync"
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
	laddr       string
	raddr       string
	ltls        bool
	rtls        bool
	tunnelCount int
	tunnels     []*yamux.Session
	tunnelsMtx  []sync.Mutex
	streamCount int32
}

func NewLocalServer(laddr, raddr string, ltls, rtls bool, tunnelCount int) (*LocalServer, error) {
	tunnels := make([]*yamux.Session, tunnelCount)
	for i := 0; i < tunnelCount; i++ {
		tunnel, err := createTunnel(raddr, rtls)
		if err != nil {
			log.Error("create yamux client error:%v", err)
			return nil, err
		}
		tunnels[i] = tunnel
	}

	ls := &LocalServer{
		laddr:       laddr,
		raddr:       raddr,
		ltls:        ltls,
		rtls:        rtls,
		tunnelCount: tunnelCount,
		tunnels:     tunnels,
		tunnelsMtx:  make([]sync.Mutex, tunnelCount),
		streamCount: 0,
	}
	return ls, nil
}

func (ls *LocalServer) Serve(certFile, keyFile string) {
	var l net.Listener
	var err error
	if ls.ltls {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			log.Error("load cert/key error:%v", err)
			return
		}
		config := tls.Config{Certificates: []tls.Certificate{cert}}
		l, err = tls.Listen("tcp", ls.laddr, &config)
	} else {
		l, err = net.Listen("tcp", ls.laddr)
	}
	if err != nil {
		log.Error("listen [%s] error:%v", ls.laddr, err)
		return
	}

	for {
		c, err := l.Accept()
		if err != nil {
			log.Error("accept connection error:%v", err)
			continue
		}
		go ls.transport(c)
	}
}

func (ls *LocalServer) transport(conn net.Conn) {
	defer conn.Close()
	start := time.Now()
	stream, err := ls.openStream(conn)
	if err != nil {
		log.Error("open stream for %s error:%v", conn.RemoteAddr().String(), err)
		return
	}
	defer stream.Close()

	atomic.AddInt32(&ls.streamCount, 1)
	streamID := stream.StreamID()
	readChan := make(chan int64, 1)
	writeChan := make(chan int64, 1)
	var readBytes int64
	var writeBytes int64
	go pipe(conn, stream, readChan)
	go pipe(stream, conn, writeChan)
	for i := 0; i < 2; i++ {
		select {
		case readBytes = <-readChan:
			// DON'T call conn.Close, it will trigger an error in pipe.
			// Just close stream, let the conn copy finished normally
			// conn.Close()
			log.Debug("[#%d] read %d bytes", streamID, readBytes)
		case writeBytes = <-writeChan:
			stream.Close()
			log.Debug("[#%d] write %d bytes", streamID, writeBytes)
		}
	}
	log.Info("[#%d] r:%d w:%d t:%v c:%d",
		streamID, readBytes, writeBytes, time.Now().Sub(start), atomic.LoadInt32(&ls.streamCount))
	atomic.AddInt32(&ls.streamCount, -1)
}

func (ls *LocalServer) openStream(conn net.Conn) (stream *yamux.Stream, err error) {
	// h, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	// idx := ip2int(net.ParseIP(h)) % ls.tunnelCount
	idx := rand.Intn(ls.tunnelCount) % ls.tunnelCount

	ls.tunnelsMtx[idx].Lock()
	defer ls.tunnelsMtx[idx].Unlock()
	stream, err = ls.tunnels[idx].OpenStream()
	if err != nil {
		if err == yamux.ErrStreamsExhausted || err == yamux.ErrSessionShutdown {
			log.Warn("[1/3]tunnel stream [%v]. [%s] to [%s], NumStreams:%d",
				err,
				ls.tunnels[idx].RemoteAddr().String(),
				ls.tunnels[idx].LocalAddr().String(),
				ls.tunnels[idx].NumStreams())
			tunnel, err := createTunnel(ls.raddr, ls.rtls)
			if err != nil {
				log.Error("[2/3]try to create new tunnel error:%v", err)
				return nil, err
			} else {
				log.Warn("[2/3]create new tunnel OK")
			}
			stream, err = tunnel.OpenStream()
			if err != nil {
				log.Error("[3/3]open Stream from new tunnel error:%v", err)
				return nil, err
			} else {
				log.Warn("[3/3]open Stream from new tunnel OK")
				go ls.serveTunnel(ls.tunnels[idx])
				ls.tunnels[idx] = tunnel
				return stream, nil
			}
		} else {
			log.Error("openStream error:%v", err)
		}
	}

	return stream, err
}

func (ls *LocalServer) serveTunnel(tunnel *yamux.Session) {
	for {
		if tunnel.IsClosed() || tunnel.NumStreams() == 0 {
			log.Warn("[1/2]tunnel colsed:%v, NumStreams:%d, exiting...", tunnel.IsClosed(), tunnel.NumStreams())
			tunnel.Close()
			log.Warn("[2/2]tunnel exit")
			return
		}
		time.Sleep(10 * time.Second)
	}
}

func ip2int(ip net.IP) int {
	if ip == nil {
		return 0
	}
	ip = ip.To4()

	var ipInt int
	ipInt += int(ip[0]) << 24
	ipInt += int(ip[1]) << 16
	ipInt += int(ip[2]) << 8
	ipInt += int(ip[3])
	return ipInt
}
