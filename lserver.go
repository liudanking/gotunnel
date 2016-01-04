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
	rConn       *net.TCPConn
	streamID    uint16
	streamMutex *sync.Mutex
	streams     map[uint16]chan Frame
	in          chan []byte
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
		laddr:       a1,
		raddr:       a2,
		streamID:    0,
		streamMutex: &sync.Mutex{},
		streams:     make(map[uint16]chan Frame),
		in:          make(chan []byte),
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
		// subscribe
		t.subscribeStream(sid)
		go t.handleLocalConn(sid, conn)
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

func (t *LocalServer) subscribeStream(sid uint16) {
	if _, ok := t.streams[sid]; ok {
		log.Warn("sid round back")
	}

	t.streams[sid] = make(chan Frame)
}

func (t *LocalServer) publishStream(f Frame) {
	if cf, ok := t.streams[f.StreamID]; !ok {
		log.Warn("stream %d not found", f.StreamID)
		return
	} else {
		cf <- f
	}
}

func (t *LocalServer) handleLocalConn(sid uint16, conn *net.TCPConn) {
	defer func() {
		conn.Close()
		delete(t.streams, sid)
		// TODO: send STOP frame
	}()
	// TODO: send heartbeat
	// stream id
	frame, ok := t.streams[sid]
	if !ok {
		log.Warn("stream %d not found", sid)
		return
	}

	// start
	buf := make([]byte, BUF_SIZE)
	n, err := conn.Read(buf[5:])
	if err != nil {
		log.Error("conn.Read error:%v", err)
		return
	} else {
		buf[0] = byte(sid >> 8)
		buf[1] = byte(sid & 0x00ff)
		buf[2] = S_START
		buf[3] = byte(uint16(n) >> 8)
		buf[4] = byte(uint16(n) & 0x00ff)
	}

	// send to LocalServer
	t.in <- buf[:5+n]

	// TODO: handle error and exceptions
	// trans local to remote
	go func() {
		for {
			// TODO: use pool
			buf := make([]byte, BUF_SIZE)
			n, err := conn.Read(buf[5:])
			if err != nil {
				log.Error("conn.Read error:%v", err)
				return
			} else {
				buf[0] = byte(sid >> 8)
				buf[1] = byte(sid & 0x00ff)
				buf[2] = S_START
				buf[3] = byte(uint16(n) >> 8)
				buf[4] = byte(uint16(n) & 0x00ff)
			}
			// send to LocalServer
			t.in <- buf
		}
	}()

	// trans remote to local
	for {
		select {
		case f := <-frame:
			n, err := conn.Write(f.Payload)
			if err != nil {
				log.Error("write remote to local error:%v", err)
				return
			}
			if n != len(f.Payload) {
				log.Warn("data length:%d, write length:%d", len(f.Payload), n)
				return
			}
		}
	}
}
