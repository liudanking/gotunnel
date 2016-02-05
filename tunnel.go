package main

import (
	"io"
	"net"

	log "github.com/liudanking/log4go"

	"time"
)

func pipe(dst net.Conn, src net.Conn, copiedBytes chan int64) {
	n, err := io.Copy(dst, src)
	if err != nil {
		log.Warn("copy [%s] to [%s] error:%v", src.RemoteAddr().String(), dst.RemoteAddr().String(), err)
	}
	err = src.Close()
	if err != nil {
		log.Warn("close [%s] error:%v", src.RemoteAddr().String(), err)
	}
	copiedBytes <- n
}

func readWritable(c net.Conn) (readable, writable bool) {
	readable = true
	writable = true
	c.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	buf := make([]byte, 1)
	_, err := c.Read(buf)
	if err != nil && err == io.EOF {
		readable = false
		log.Debug("detect readable error:%v", err)
	}
	c.SetReadDeadline(time.Time{})

	c.SetWriteDeadline(time.Now().Add(10 * time.Millisecond))
	if _, err := c.Write(buf); err != nil {
		writable = false
		log.Debug("detect writable error:%v", err)
	}
	c.SetWriteDeadline(time.Time{})
	return
}
