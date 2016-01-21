package main

import (
	"io"

	"github.com/liudanking/gotunnel/bytes"

	log "github.com/liudanking/log4go"
)

const (
	HEADER_SIZE = 5
	BUF_SIZE    = HEADER_SIZE + 8192
	BUF_NUM     = 1024
)

type Frame struct {
	*bytes.Buffer
}

func (f *Frame) StreamID() uint16 {
	buf := f.Bytes()
	return uint16(buf[0])<<8 + uint16(buf[1])
}

func (f *Frame) Cmd() byte {
	return f.Bytes()[2]
}

func (f *Frame) Length() uint16 {
	buf := f.Bytes()
	return uint16(buf[3])<<8 + uint16(buf[4])
}

func (f *Frame) Payload() []byte {
	return f.Bytes()[HEADER_SIZE : HEADER_SIZE+f.Length()]
}

func (f *Frame) Bytes() []byte {
	buf := f.Buffer.Bytes()
	return buf[:BUF_SIZE]
}

func (f *Frame) Data() []byte {
	return f.Bytes()[:HEADER_SIZE+f.Length()]
}

var bPool *bytes.Pool = bytes.NewPool(BUF_NUM, BUF_SIZE)

func getBuffer() *bytes.Buffer {
	return bPool.Get()
}

func putBuffer(b *bytes.Buffer) {
	bPool.Put(b)
}

func readFrame(r io.Reader, f *Frame) error {
	buf := f.Bytes()
	err := readBytes(r, buf[:HEADER_SIZE])
	if err != nil {
		log.Error("read frame header error:%v", err)
		return err
	}

	if f.StreamID() > 100 || f.Length() > 8192 {
		log.Warn("[1/2] read a frame: stream %d, cmd:%d, length:%d", f.StreamID(), f.Cmd(), f.Length())
		log.Warn("[2/2] read a frame, header:%v", buf[:5+20])
	}

	// log.Debug("[1/2] read a frame: stream %d, cmd:%d, length:%d", f.StreamID(), f.Cmd(), length)
	// log.Debug("[2/2] read a frame, header:%v %v", f.Bytes()[:5], buf[:5])

	length := f.Length()
	if length == 0 {
		return nil
	}
	err = readBytes(r, buf[HEADER_SIZE:HEADER_SIZE+length])
	if err != nil {
		log.Error("read frame payload error:%v", err)
		return err
	}
	return nil
}

func readBytes(r io.Reader, p []byte) error {
	var (
		nn  int
		n   int
		err error
	)
	length := len(p)
	for nn < length && err == nil {
		n, err = r.Read(p)
		if err != nil {
			log.Warn("readBytes error:%v", err)
			return err
		}
		nn += n
		p = p[n:]
	}

	if nn != length {
		log.Warn("readBytes %d != %d", nn, length)
	}

	return nil
}

func writeBytes(w io.Writer, p []byte) error {
	var (
		nn  int
		n   int
		err error
	)
	length := len(p)
	for nn < length && err == nil {
		n, err = w.Write(p)
		if err != nil {
			log.Warn("writeBytes error:%v", err)
			return err
		}
		nn += n
		p = p[n:]
	}

	if nn != length {
		log.Warn("writeBytes %d != %d", nn, length)
	}

	return nil
}

func frameHeader(sid uint16, status byte, length uint16, buf []byte) {
	if len(buf) < HEADER_SIZE {
		log.Error("buf is too small")
		return
	}
	buf[0] = byte(sid >> 8)
	buf[1] = byte(sid & 0x00ff)
	buf[2] = status
	buf[3] = byte(length >> 8)
	buf[4] = byte(length & 0x00ff)
}
