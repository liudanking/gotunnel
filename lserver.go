package main

import (
	"net"
	"sync"
	"time"

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
	rConn        *net.TCPConn
	streamID     uint16
	streamMutex  *sync.Mutex
	streams      map[uint16]chan Frame
	streamMapMtx *sync.Mutex
	// in           chan []byte
	in chan Frame
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
		in:           make(chan Frame), //make(chan []byte),
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
		log.Info("tunnel established")

		// receive local packets and send to remote
		go func() {
			defer func() {
				log.Info("local server exit")
			}()
			for {
				select {
				case f := <-ls.in:
					err := writeBytes(ls.rConn, f.Data())
					if err != nil {
						log.Error("conn.write error:%v", err)
						return
					}
					putBuffer(f.Buffer)
				}
			}
		}()

		f := Frame{}
		for {
			f.Buffer = getBuffer()
			err := readFrame(ls.rConn, &f)
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
			ls.publishStream(f)
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
		putBuffer(f.Buffer)
		if err := recover(); err != nil {
			// this happens when write on closed channel cf
			log.Error("panic in publishStream:%v", err)
		}
	}()
	if cf, ok := ls.getStream(f.StreamID()); !ok {
		log.Warn("stream %d not found", f.StreamID())
		return
	} else {
		cf <- f
	}
}

func (ls *LocalServer) handleLocalConn(sid uint16, frame chan Frame, conn *net.TCPConn) {
	defer func() {
		conn.Close()
		close(frame)
		ls.delStream(sid)
		f := Frame{Buffer: getBuffer()}
		frameHeader(sid, S_STOP, 0, f.Bytes())
		ls.in <- f
		log.Debug("handleLocalConn exit, stream: %d", sid)
	}()

	// TODO: send heartbeat

	// trans local to remote
	readFinished := make(chan struct{})
	go func() {
		firstMsg := true
		f := Frame{}
		for {
			f.Buffer = getBuffer()
			buf := f.Bytes()
			n, err := conn.Read(buf[HEADER_SIZE:])
			if err != nil {
				// if err != io.EOF {
				log.Info("conn.Read error:%v", err)
				// }
				conn.CloseRead()
				break
			} else {
				if firstMsg {
					frameHeader(sid, S_START, uint16(n), buf)
					firstMsg = false
				} else {
					frameHeader(sid, S_TRANS, uint16(n), buf)
				}
			}
			log.Info("read %d bytes", n)
			ls.in <- f
		}
		readFinished <- struct{}{}
	}()

	// trans remote to local
	for {
		select {
		case f, ok := <-frame:
			if !ok {
				log.Info("stream %d channel closed", sid)
				return
			}
			log.Debug("receive a frame, %d bytes", f.Length())

			switch f.Cmd() {
			case S_START, S_TRANS:
				err := writeBytes(conn, f.Payload())
				if err != nil {
					log.Error("write remote to local error:%v", err)
					conn.CloseWrite()
					goto end
				}
			case S_STOP:
				conn.CloseWrite()
				goto end
			}
		}
	}

end:
	<-readFinished
}
