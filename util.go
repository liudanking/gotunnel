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
		StreamID: uint16(buf[0]<<8 + buf[1]),
		Cmd:      buf[2],
		Length:   uint16(buf[3]<<8 + buf[4]),
	}
	log.Debug("read a frame:%+v", f)
	n, err = r.Read(buf[:f.Length])
	if err != nil {
		return nil, err
	}
	if n != int(f.Length) {
		log.Error("read %d bytes, expect read %d bytes", n, f.Length)
		return nil, errors.New("read frame payload error")
	}

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
