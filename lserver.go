package main

import (
	"net"
	"sync"
	"time"

	log "github.com/liudanking/log4go"
)

const (
	BUF_SIZE = 8192
	S_START  = 0x01
	S_TRANS  = 0x02
	S_HEART  = 0Xfe
	S_STOP   = 0Xff
)

type Frame struct {
	StreamID uint16
	Cmd      uint8
	Length   uint16
	Payload  []byte
}

type LocalServer struct {
	laddr *net.TCPAddr
	raddr *net.TCPAddr
	// remote connection, only used in local mode
	rConn        *net.TCPConn
	streamID     uint16
	streamMutex  *sync.Mutex
	streams      map[uint16]chan Frame
	streamMapMtx *sync.Mutex
	in           chan []byte
}

func NewLocalServer(laddr, raddr string) (*LocalServer, error) {
	a1, err := net.ResolveTCPAddr("tcp", laddr)
	if err != nil {
		return nil, err
	}
	a2, err := net.ResolveTCPAddr("tcp", raddr)
	if err != nil {
		return nil, err
	}

	ls := &LocalServer{
		laddr:        a1,
		raddr:        a2,
		streamID:     0,
		streamMutex:  &sync.Mutex{},
		streams:      make(map[uint16]chan Frame),
		streamMapMtx: &sync.Mutex{},
		in:           make(chan []byte),
	}
	return ls, nil
}

func (t *LocalServer) serve() {
	// establish a local to remote LocalServer
	go t.tunnel()

	// start local listenner
	l, err := net.ListenTCP("tcp", t.laddr)
	if err != nil {
		log.Error("net.ListenTCP(tcp, %s) error:%v", t.laddr, err)
		return
	}

	for {
		conn, err := l.AcceptTCP()
		if err != nil {
			log.Error("l.AcceptTCP() error:%v", err)
			continue
		}
		sid := t.nextStreamID()
		log.Debug("accept a connection:%s, stream %d", conn.RemoteAddr().String(), sid)

		// subscribe
		f := t.subscribeStream(sid)
		go t.handleLocalConn(sid, f, conn)
	}
}

func (ls *LocalServer) tunnel() {
	for {
		// establish TCP connection
		conn, err := net.DialTCP("tcp", nil, ls.raddr)
		if err != nil {
			log.Warn("net.DialTCP(%s) error:%v", ls.raddr.String(), err)
			time.Sleep(5 * time.Second)
			continue
		}
		ls.rConn = conn

		// receive local packets and send to remote
		go func() {
			defer func() {
				log.Info("local server exit")
			}()
			for {
				select {
				case data := <-ls.in:
					_, err := writeBytes(ls.rConn, data)
					if err != nil {
						log.Error("conn.write error:%v", err)
						return
					}
				}
			}
		}()

		for {
			// TODO: use pool
			f, err := readFrame(ls.rConn)
			if err != nil {
				log.Error("rConn read error:%v", err)
				//  clear
				for k, v := range ls.streams {
					close(v)
					delete(ls.streams, k)
				}
				break
			}

			// publish to stream
			ls.publishStream(*f)
		}
	}
}

func (t *LocalServer) nextStreamID() uint16 {
	t.streamMutex.Lock()
	t.streamID += 1
	t.streamMutex.Unlock()
	return t.streamID
}

func (ls *LocalServer) getStream(sid uint16) (f chan Frame, ok bool) {
	ls.streamMapMtx.Lock()
	f, ok = ls.streams[sid]
	ls.streamMapMtx.Unlock()
	return f, ok
}

func (ls *LocalServer) delStream(sid uint16) {
	ls.streamMapMtx.Lock()
	delete(ls.streams, sid)
	ls.streamMapMtx.Unlock()
}

func (ls *LocalServer) subscribeStream(sid uint16) (f chan Frame) {
	ls.streamMapMtx.Lock()
	if _, ok := ls.streams[sid]; ok {
		// TODO: solve sid round back problem
		log.Warn("sid round back")
	}
	f = make(chan Frame)
	ls.streams[sid] = f
	ls.streamMapMtx.Unlock()
	return f
}

func (ls *LocalServer) publishStream(f Frame) {
	defer func() {
		if err := recover(); err != nil {
			// this happens write on closed channel cf
			log.Error("panic in publishStream:%v", err)
		}
	}()
	if cf, ok := ls.getStream(f.StreamID); !ok {
		log.Warn("stream %d not found", f.StreamID)
		return
	} else {
		cf <- f
	}
}

func (t *LocalServer) handleLocalConn(sid uint16, frame chan Frame, conn *net.TCPConn) {
	buf := make([]byte, BUF_SIZE)
	defer func() {
		conn.Close()
		t.delStream(sid)
		close(frame)
		frameHeader(sid, S_STOP, 0, buf)
		t.in <- buf[:5]
		log.Debug("handleLocalConn exit, stream: %d", sid)
	}()

	// TODO: send heartbeat

	// trans local to remote
	go func() {
		firstMsg := true
		for {
			// TODO: use pool
			buf := make([]byte, BUF_SIZE)
			n, err := conn.Read(buf[5:])
			if err != nil {
				log.Info("conn.Read error:%v", err)
				conn.CloseRead()
				return
			} else {
				if firstMsg {
					frameHeader(sid, S_START, uint16(n), buf)
					firstMsg = false
				} else {
					frameHeader(sid, S_TRANS, uint16(n), buf)
				}
			}
			log.Info("read %d bytes", n)
			// send to LocalServer
			t.in <- buf[:5+n]
		}
	}()

	// trans remote to local
	for {
		select {
		case f, ok := <-frame:
			if !ok {
				log.Info("channel closed")
				return
			}
			log.Debug("receive a frame, %d bytes", f.Length)
			switch f.Cmd {
			case S_START, S_TRANS:
				n, err := conn.Write(f.Payload)
				if err != nil {
					log.Error("write remote to local error:%v", err)
					conn.CloseWrite()
					return
				}
				if n != len(f.Payload) {
					log.Warn("data length:%d, write length:%d", len(f.Payload), n)
					return
				}
			case S_STOP:
				return
			}
		}
	}
}
