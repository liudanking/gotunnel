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
	// in           chan []byte
	in chan Frame
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
		in:           make(chan Frame),
	}
	return rs, nil
}

func (r *RemoteServer) getStream(sid uint16) (f chan Frame, ok bool) {
	r.streamMapMtx.Lock()
	f, ok = r.streams[sid]
	r.streamMapMtx.Unlock()
	return f, ok
}

func (r *RemoteServer) setStream(sid uint16, f chan Frame) {
	r.streamMapMtx.Lock()
	r.streams[sid] = f
	r.streamMapMtx.Unlock()
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
		_f := Frame{}
		for {
			_f.Buffer = getBuffer()
			err := readFrame(conn, &_f)
			if err != nil {
				log.Error("readFrame error:%v", err)
				return
			}
			streamID := _f.StreamID()
			cmd := _f.Cmd()
			length := _f.Length()
			log.Debug("frame stream %d, cmd %d, length %d", streamID, cmd, length)

			switch cmd {
			case S_START:
				c, err := net.DialTCP("tcp", nil, r.raddr)
				if err != nil {
					log.Error("net.DialTCP(%s) error:%v", r.raddr.String(), err)
					buf := make([]byte, 5)
					frameHeader(streamID, S_STOP, 0, buf)
					r.in <- _f
					continue
				}
				frame := make(chan Frame)
				r.setStream(streamID, frame)
				go r.handleRemoteConn(streamID, c, frame)
				log.Debug("start stream %d", streamID)
				frame <- _f
			case S_TRANS:
				if frame, ok := r.getStream(streamID); ok {
					frame <- _f
				} else {
					log.Error("frame channel of stream %d not found", streamID)
				}
			case S_STOP:
				if f, ok := r.getStream(streamID); ok {
					close(f)
					log.Debug("stream %d channel closed", streamID)
				}

				r.delStream(streamID)
				log.Debug("stream %d deleted", streamID)
			}
		}
	}()

	for {
		select {
		case f := <-r.in:
			// log.Debug("r.in")
			err := writeBytes(conn, f.Data())
			if err != nil {
				log.Error("conn.Write error:%v", err)
				return
			}
			putBuffer(f.Buffer)
		}
	}
}

func (r *RemoteServer) handleRemoteConn(sid uint16, conn *net.TCPConn, frame chan Frame) {
	writeFinished := make(chan struct{})
	go func() {
		defer func() {
			conn.Close()
			close(frame)
			r.delStream(sid)
			f := Frame{Buffer: getBuffer()}
			frameHeader(sid, S_STOP, 0, f.Bytes())
			r.in <- f
		}()
		for {
			select {
			case f, ok := <-frame:
				if !ok {
					// conn.Close()
					// r.delStream(sid)
					log.Debug("stream %d channel closed", sid)
					return
				}
				streamID := f.StreamID()
				cmd := f.Cmd()
				length := f.Length()
				log.Debug("receive a frame stream %d, cmd %d, length %d", streamID, cmd, length)
				switch cmd {
				case S_START, S_TRANS:
					err := writeBytes(conn, f.Payload())
					if err != nil {
						log.Error("conn.Write error:%v", err)
						conn.CloseWrite()
						// return
						goto end
					}
				case S_STOP:
					// conn.Close()
					// r.delStream(sid)
					// close(frame)
					conn.CloseWrite()
					goto end
				}
				putBuffer(f.Buffer)
			}
		}
	end:
		writeFinished <- struct{}{}
	}()

	f := Frame{}
	for {
		f.Buffer = getBuffer()
		buf := f.Bytes()
		n, err := conn.Read(buf[HEADER_SIZE:])
		if err != nil {
			log.Error("conn.read error:%v", err)
			conn.CloseRead()
			break
		}
		log.Debug("read a msg %d bytes", n)
		frameHeader(sid, S_TRANS, uint16(n), buf)
		// log.Debug("header: stream %d, length %d", sid, n)
		r.in <- f
		log.Debug("send to tunnel")
	}

	<-writeFinished
}
