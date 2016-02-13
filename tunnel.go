package main

import (
	"crypto/tls"
	"io"
	"net"
	"time"

	"github.com/hashicorp/yamux"
	log "github.com/liudanking/log4go"
)

var TLS_SESSION_CACHE tls.ClientSessionCache = tls.NewLRUClientSessionCache(32)

func createTunnel(addr string, secure bool) (*yamux.Session, error) {
	if secure {
		return createTLSTunnel(addr)
	}
	return createTCPTunnel(addr)
}

// createTCPTunnel establishes a TCP connection to addr.
func createTCPTunnel(addr string) (*yamux.Session, error) {
	start := time.Now()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	log.Info("tcp connection cost:%v", time.Now().Sub(start))
	// TODO: config client
	return yamux.Client(conn, nil)
}

// createTLSTunnel establishes a TLS connection to addr.
// addr should be a domain, otherwise ServerName should be set
func createTLSTunnel(addr string) (*yamux.Session, error) {
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
	return yamux.Client(conn, nil)
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
