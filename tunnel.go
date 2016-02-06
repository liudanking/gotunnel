package main

import (
	"crypto/tls"
	"io"
	"net"

	log "github.com/liudanking/log4go"
	"github.com/liudanking/yamux"

	"time"
)

var TLS_SESSION_CACHE tls.ClientSessionCache = tls.NewLRUClientSessionCache(32)

// createTunnel establishes a TLS connection to addr.
// addr should be a domain, otherwise ServerName should be set
func createTunnel(addr string) (*yamux.Session, error) {
	start := time.Now()
	config := tls.Config{
		InsecureSkipVerify: DEBUG,
		ClientSessionCache: TLS_SESSION_CACHE, // use sessoin ticket to speed up tls handshake
	}
	conn, err := tls.Dial("tcp", addr, &config)
	if err != nil {
		return nil, err
	}
	cs := conn.ConnectionState()
	log.Info("tls connection: resume:%v, ciphersuite:0x%02x, cost:%v",
		cs.DidResume, cs.CipherSuite, time.Now().Sub(start))
	// TODO: config client
	cfg := yamux.DefaultConfig()
	cfg.MaxStreamWindowSize *= 1
	return yamux.Client(conn, cfg)
}

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
