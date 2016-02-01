package main

import (
	"io"
	"net"
	"sync"

	log "github.com/liudanking/log4go"
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
		f := Frame{}
		for {
			f.Buffer = getBuffer()
			err := readFrame(conn, &f)
			if err != nil {
				log.Error("readFrame error:%v", err)
				return
			}
			streamID := f.StreamID()
			cmd := f.Cmd()
			length := f.Length()
			log.Debug("frame stream %d, cmd %d, length %d", streamID, cmd, length)

			switch cmd {
			case S_START:
				c, err := net.DialTCP("tcp", nil, r.raddr)
				if err != nil {
					log.Error("net.DialTCP(%s) error:%v", r.raddr.String(), err)
					f.Buffer = getBuffer()
					frameHeader(streamID, S_STOP, 0, f.Bytes())
					r.in <- f
					continue
				}
				frame := r.subscribeStream(streamID)
				go r.handleRemoteConn(streamID, c, frame)
				log.Debug("start stream %d", streamID)
				r.publishStream(f)
			case S_TRANS, S_STOP:
				r.publishStream(f)
			}
		}
	}()

	for {
		select {
		case f := <-r.in:
			err := writeBytes(conn, f.Data())
			if err != nil {
				log.Error("conn.Write error:%v", err)
				return
			}
			putBuffer(f.Buffer)
		}
	}
}

func (r *RemoteServer) subscribeStream(sid uint16) (f chan Frame) {
	r.streamMapMtx.Lock()
	if _, ok := r.streams[sid]; ok {
		// TODO: solve sid round back problem
		log.Warn("sid round back")
	}
	f = make(chan Frame)
	r.streams[sid] = f
	r.streamMapMtx.Unlock()
	return f
}

func (r *RemoteServer) publishStream(f Frame) {
	defer func() {
		if err := recover(); err != nil {
			putBuffer(f.Buffer)
			// this happens when write on closed channel cf
			log.Error("panic in publishStream:%v", err)
		}
	}()
	if cf, ok := r.getStream(f.StreamID()); !ok {
		log.Warn("stream %d not found", f.StreamID())
		return
	} else {
		cf <- f
	}
}

func (r *RemoteServer) handleRemoteConn(sid uint16, conn *net.TCPConn, frame chan Frame) {
	defer func() {
		conn.Close()
		close(frame)
		r.delStream(sid)
		// f := Frame{Buffer: getBuffer()}
		// frameHeader(sid, S_STOP, 0, f.Bytes())
		// r.in <- f
		log.Debug("handleRemoteConn exit, stream: %d", sid)
	}()
	log.Debug("arg0")

	readFinished := make(chan struct{})
	go func() {
		f := Frame{}
		for {
			f.Buffer = getBuffer()
			buf := f.Bytes()
			n, err := conn.Read(buf[HEADER_SIZE:])
			if err != nil {
				if err != io.EOF {
					log.Error("conn.read error:%v", err)
				}
				conn.CloseRead()
				break
			}
			log.Debug("stream %d read a msg %d bytes", n, sid)
			frameHeader(sid, S_TRANS, uint16(n), buf)
			r.in <- f
			log.Debug("send to tunnel")
		}
		readFinished <- struct{}{}
	}()

	for {
		select {
		case f, ok := <-frame:
			if !ok {
				putBuffer(f.Buffer)
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
				putBuffer(f.Buffer)
				if err != nil {
					log.Error("conn.Write error:%v", err)
					conn.CloseWrite()
					goto end
				}
			case S_STOP:
				putBuffer(f.Buffer)
				log.Info("stream %d receive STOP frame", sid)
				return
			}
		}
	}

end:
	<-readFinished
}
