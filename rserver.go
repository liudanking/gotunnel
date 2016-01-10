package main

import (
	log "github.com/liudanking/log4go"

	"net"
)

type RemoteServer struct {
	laddr   *net.TCPAddr
	raddr   *net.TCPAddr
	streams map[uint16]chan Frame
	in      chan []byte
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
		laddr:   a1,
		raddr:   a2,
		streams: make(map[uint16]chan Frame),
		in:      make(chan []byte),
	}
	return rs, nil
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
			if f, ok := r.streams[_f.StreamID]; ok {
				frame = f
			} else {
				c, err := net.DialTCP("tcp", nil, r.raddr)
				if err != nil {
					log.Error("net.DialTCP(%s) error:%v", r.raddr.String(), err)
					// TODO: send STOP frame
					continue
				}
				frame = make(chan Frame)
				r.streams[_f.StreamID] = frame
				go r.handleRemoteConn(_f.StreamID, c, r.streams[_f.StreamID])
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
				n, err := writeBytes(conn, f.Payload)
				if err != nil {
					log.Error("conn.Write error:%v", err)
					return
				}
				log.Debug("write %d bytes", n)
			}
		}
	}()

	for {
		buf := make([]byte, BUF_SIZE+5)
		n, err := conn.Read(buf[5:])
		if err != nil {
			log.Error("conn.read error:%v", err)
			conn.Close()
			return
		}
		log.Debug("read msg:%v", buf[:5+n])

		buf[0] = byte(sid >> 8)
		buf[1] = byte(sid & 0x00ff)
		buf[2] = S_TRANS
		buf[3] = byte(n >> 8)
		buf[4] = byte(n & 0x00ff)
		r.in <- buf[:5+n]

	}
}
