package main

import (
	"crypto/tls"
	"time"

	"github.com/hashicorp/yamux"
	log "github.com/liudanking/log4go"
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
	return yamux.Client(conn, nil)
}
