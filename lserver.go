package main

import (
	"io"
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
	in           chan Frame
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
					putBuffer(f.Buffer)
					if err != nil {
						log.Error("conn.write error:%v", err)
						return
					}
				}
			}
		}()

		// remote to local
		f := Frame{}
		for {
			f.Buffer = getBuffer()
			err := readFrame(ls.rConn, &f)
			if err != nil {
				log.Error("rConn read error:%v", err)
				//  clear
				ls.streamMapMtx.Lock()
				for k, v := range ls.streams {
					close(v) // notify connectioin exit
					delete(ls.streams, k)
				}
				ls.streamMapMtx.Unlock()
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
		if err := recover(); err != nil {
			// when panic happens, put buffer back to avoid possible memory leak
			putBuffer(f.Buffer)
			// this happens when write on closed channel cf
			log.Error("panic in publishStream:%v", err)
		}
	}()
	if cf, ok := ls.getStream(f.StreamID()); !ok {
		log.Warn("stream %d not found", f.StreamID())
		// telll remote server stop send frame with the stream id
		frame := Frame{Buffer: getBuffer()}
		frameHeader(f.StreamID(), S_STOP, 0, frame.Bytes())
		ls.in <- frame
		log.Debug("tell remote to stop %d", f.StreamID())
		return
	} else {
		cf <- f
	}
}

func (ls *LocalServer) handleLocalConn(sid uint16, frame chan Frame, conn *net.TCPConn) {
	// defer func() {
	// 	conn.Close()
	// 	close(frame)
	// 	ls.delStream(sid)
	// 	f := Frame{Buffer: getBuffer()}
	// 	frameHeader(sid, S_STOP, 0, f.Bytes())
	// 	ls.in <- f
	// 	log.Debug("handleLocalConn exit, stream: %d", sid)
	// }()

	// TODO: send heartbeat

	// trans local to remote
	readFinished := make(chan struct{})
	go func() {
		defer func() {
			readFinished <- struct{}{}
			// TODO
			recover()
		}()
		var err error
		var status byte = S_START
		f := Frame{}
		for ; ; status = S_TRANS {
			f.Buffer = getBuffer()
			err = readToFrame(conn, &f, sid, status)
			if err != nil {
				if err != io.EOF {
					log.Info("conn.Read error:%v", err)
				}
				conn.CloseRead()
				return
			}
			log.Debug("read %d bytes", f.Length())
			ls.in <- f
		}
	}()

	// trans remote to local
	sendStop := false
	for {
		select {
		case f, ok := <-frame:
			if !ok { // tunnel is closed
				putBuffer(f.Buffer)
				conn.Close()
				log.Info("stream %d channel closed", sid)
				goto end
			}
			log.Debug("receive a frame, %d bytes", f.Length())
			switch f.Cmd() {
			case S_START, S_TRANS:
				err := writeBytes(conn, f.Payload())
				putBuffer(f.Buffer)
				if err != nil {
					log.Error("write remote to local error:%v", err)
					conn.CloseWrite()
					close(frame)
					ls.delStream(sid)
					sendStop = true
					goto end
				}
			case S_STOP:
				putBuffer(f.Buffer)
				conn.Close()
				close(frame)
				ls.delStream(sid)
				goto end
			}
		}
	}

end:
	<-readFinished
	if sendStop {
		ls.in <- stopFrame(sid)
	}
}
