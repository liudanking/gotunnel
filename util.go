package main

import (
	"errors"
	"io"

	log "github.com/liudanking/log4go"
)

func readFrame(r io.Reader) (*Frame, error) {
	buf := make([]byte, BUF_SIZE)
	n, err := r.Read(buf[:5])
	if err != nil {
		return nil, err
	}
	if n != 5 {
		log.Error("read %d bytes, expect read 5 bytes", n)
		return nil, errors.New("read frame header error")
	}
	f := &Frame{
		StreamID: uint16(buf[0])<<8 + uint16(buf[1]),
		Cmd:      buf[2],
		Length:   uint16(buf[3])<<8 + uint16(buf[4]),
	}
	log.Debug("read a frame:%+v", f)
	n, err = r.Read(buf[:f.Length])
	if err != nil {
		log.Error("read eror:%v", err)
		return nil, err
	}
	if n != int(f.Length) {
		log.Error("read %d bytes, expect read %d bytes", n, f.Length)
		return nil, errors.New("read frame payload error")
	}
	f.Payload = buf[:f.Length]
	return f, nil
}

func writeBytes(w io.Writer, p []byte) (int, error) {
	n, err := w.Write(p)
	if err != nil {
		return 0, err
	}
	if n != len(p) {
		log.Warn("write %d bytes, expect %d bytes", n, len(p))
	}

	return n, nil
}

func frameHeader(sid uint16, status byte, length uint16, buf []byte) {
	if len(buf) < 5 {
		log.Error("buf is too small")
		return
	}
	buf[0] = byte(sid >> 8)
	buf[1] = byte(sid & 0x00ff)
	buf[2] = status
	buf[3] = byte(length >> 8)
	buf[4] = byte(length & 0x00ff)
}
