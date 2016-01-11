package main

import (
	"sync"

	log "github.com/liudanking/log4go"

	"net"
)

type RemoteServer struct {
	laddr        *net.TCPAddr
	raddr        *net.TCPAddr
	streams      map[uint16]chan Frame
	streamMapMtx *sync.Mutex
	in           chan []byte
}

func NewRemoteServer(laddr, raddr string) (*RemoteServer, error) {
	a1, err := net.ResolveTCPAddr("tcp", laddr)
	if err != nil {
		return nil, err
	}
	a2, err := net.ResolveTCPAddr("tcp", raddr)
	if err != nil {
		return nil, err
	}

	rs := &RemoteServer{
		laddr:        a1,
		raddr:        a2,
		streams:      make(map[uint16]chan Frame),
		streamMapMtx: &sync.Mutex{},
		in:           make(chan []byte),
	}
	return rs, nil
}

func (r *RemoteServer) getStream(sid uint16) (f chan Frame, ok bool) {
	r.streamMapMtx.Lock()
	f, ok = r.streams[sid]
	r.streamMapMtx.Unlock()
	return f, ok
}

func (r *RemoteServer) delStream(sid uint16) {
	r.streamMapMtx.Lock()
	delete(r.streams, sid)
	r.streamMapMtx.Unlock()
}

func (r *RemoteServer) serve() {
	l, err := net.ListenTCP("tcp", r.laddr)
	if err != nil {
		log.Error("net.ListenTCP(%s) error:%v", r.laddr.String(), err)
		return
	}

	for {
		conn, err := l.AcceptTCP()
		if err != nil {
			log.Error("listenner accept connection error:%v", err)
			continue
		}
		log.Debug("accept a connection:%s", conn.RemoteAddr().String())
		// TODO: support concurrent
		go r.handleConn(conn)
	}
}

func (r *RemoteServer) handleConn(conn *net.TCPConn) {
	go func() {
		for {
			_f, err := readFrame(conn)
			if err != nil {
				log.Error("readFrame error:%v", err)
				return
			}
			var frame chan Frame
			if _f.Cmd == S_START {
				c, err := net.DialTCP("tcp", nil, r.raddr)
				if err != nil {
					log.Error("net.DialTCP(%s) error:%v", r.raddr.String(), err)
					buf := make([]byte, 5)
					frameHeader(_f.StreamID, S_STOP, 0, buf)
					r.in <- buf
					continue
				}
				frame = make(chan Frame)
				r.streams[_f.StreamID] = frame
				go r.handleRemoteConn(_f.StreamID, c, r.streams[_f.StreamID])
			} else {
				if f, ok := r.getStream(_f.StreamID); ok {
					frame = f
				} else {
					log.Error("frame channel of stream %d not found", _f.StreamID)
					continue
				}
			}
			log.Debug("frame data:%v", _f.Payload)
			frame <- *_f
		}
	}()

	for {
		select {
		case data := <-r.in:
			log.Debug("r.in")
			_, err := writeBytes(conn, data)
			if err != nil {
				log.Error("conn.Write error:%v", err)
				return
			}
		}
	}
}

func (r *RemoteServer) handleRemoteConn(sid uint16, conn *net.TCPConn, frame chan Frame) {
	go func() {
		for {
			select {
			case f := <-frame:
				log.Debug("receive a frame:%++v", f)
				switch f.Cmd {
				case S_START, S_TRANS:
					n, err := writeBytes(conn, f.Payload)
					if err != nil {
						log.Error("conn.Write error:%v", err)
						conn.CloseWrite()
						return
					}
					log.Debug("write %d bytes", n)
				case S_STOP:
					conn.Close()
					r.delStream(sid)
					close(frame)
				}
			}
		}
	}()

	for {
		buf := make([]byte, BUF_SIZE+5)
		n, err := conn.Read(buf[5:])
		if err != nil {
			log.Error("conn.read error:%v", err)
			conn.CloseRead()
			return
		}
		log.Debug("read a msg (%d bytes):%s", n, string(buf[5:5+n]))
		frameHeader(sid, S_TRANS, uint16(n), buf)
		log.Debug("header:%+v", buf[:5])
		r.in <- buf[:5+n]

	}
}
