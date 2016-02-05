package main

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/yamux"

	log "github.com/liudanking/log4go"
)

const (
	S_START = 0x01
	S_TRANS = 0x02
	S_HEART = 0Xfe
	S_STOP  = 0Xff
)

type LocalServer struct {
	laddr       *net.TCPAddr
	raddr       *net.TCPAddr
	tunnelCount int
	tunnels     []*yamux.Session
	tunnelsMtx  []sync.Mutex
	streamCount int32
}

func NewLocalServer(laddr, raddr string, tunnelCount int) (*LocalServer, error) {
	_laddr, err := net.ResolveTCPAddr("tcp", laddr)
	if err != nil {
		return nil, err
	}
	_raddr, err := net.ResolveTCPAddr("tcp", raddr)
	if err != nil {
		return nil, err
	}

	tunnels := make([]*yamux.Session, tunnelCount)
	for i := 0; i < tunnelCount; i++ {
		tunnel, err := createTunnel(_raddr)
		if err != nil {
			log.Error("create yamux client error:%v", err)
			return nil, err
		}
		tunnels[i] = tunnel
	}

	ls := &LocalServer{
		laddr:       _laddr,
		raddr:       _raddr,
		tunnelCount: tunnelCount,
		tunnels:     tunnels,
		tunnelsMtx:  make([]sync.Mutex, tunnelCount),
		streamCount: 0,
	}
	return ls, nil
}

func (ls *LocalServer) Serve() {
	l, err := net.ListenTCP("tcp", ls.laddr)
	if err != nil {
		log.Error("listen [%s] error:%v", ls.laddr.String(), err)
		return
	}

	for {
		c, err := l.AcceptTCP()
		if err != nil {
			log.Error("accept connection error:%v", err)
			continue
		}
		go ls.transport(c)
	}
}

func (ls *LocalServer) transport(conn *net.TCPConn) {
	defer conn.Close()
	start := time.Now()
	stream, err := ls.openStream(conn)
	if err != nil {
		log.Error("open stream for %s error:%v", conn.RemoteAddr().String(), err)
		return
	}
	defer stream.Close()

	atomic.AddInt32(&ls.streamCount, 1)
	streamID := stream.StreamID()
	readChan := make(chan int64, 1)
	writeChan := make(chan int64, 1)
	var readBytes int64
	var writeBytes int64
	go pipe(conn, stream, readChan)
	go pipe(stream, conn, writeChan)
	for i := 0; i < 2; i++ {
		select {
		case readBytes = <-readChan:
			// DON'T call conn.Close, it will trigger an error in pipe.
			// Just close stream, let the conn copy finished normally
			// conn.Close()
			log.Debug("[#%d] read %d bytes", streamID, readBytes)
		case writeBytes = <-writeChan:
			stream.Close()
			log.Debug("[#%d] write %d bytes", streamID, writeBytes)
		}
	}
	log.Info("[#%d] r:%d w:%d t:%v c:%d",
		streamID, readBytes, writeBytes, time.Now().Sub(start), atomic.LoadInt32(&ls.streamCount))
	atomic.AddInt32(&ls.streamCount, -1)
}

func (ls *LocalServer) openStream(conn net.Conn) (stream *yamux.Stream, err error) {
	h, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	idx := ip2int(net.ParseIP(h)) % ls.tunnelCount

	ls.tunnelsMtx[idx].Lock()
	defer ls.tunnelsMtx[idx].Unlock()
	stream, err = ls.tunnels[idx].OpenStream()
	if err != nil {
		if err == yamux.ErrStreamsExhausted || err == yamux.ErrSessionShutdown {
			log.Warn("[1/3]tunnel stream [%v]. [%s] to [%s], NumStreams:%d",
				err,
				ls.tunnels[idx].RemoteAddr().String(),
				ls.tunnels[idx].LocalAddr().String(),
				ls.tunnels[idx].NumStreams())
			tunnel, err := createTunnel(ls.raddr)
			if err != nil {
				log.Error("[2/3]try to create new tunnel error:%v", err)
				return nil, err
			} else {
				log.Warn("[2/3]create new tunnel OK")
			}
			stream, err = tunnel.OpenStream()
			if err != nil {
				log.Error("[3/3]open Stream from new tunnel error:%v", err)
				return nil, err
			} else {
				log.Warn("[3/3]open Stream from new tunnel OK")
				go ls.serveTunnel(ls.tunnels[idx])
				ls.tunnels[idx] = tunnel
				return stream, nil
			}
		} else {
			log.Error("openStream error:%v", err)
		}
	}

	return stream, err
}

func (ls *LocalServer) serveTunnel(tunnel *yamux.Session) {
	for {
		if tunnel.IsClosed() || tunnel.NumStreams() == 0 {
			log.Warn("[1/2]tunnel colsed:%v, NumStreams:%d, exiting...", tunnel.IsClosed(), tunnel.NumStreams())
			tunnel.Close()
			log.Warn("[2/2]tunnel exit")
			return
		}
		time.Sleep(10 * time.Second)
	}
}

var ROOTCA string = `
-----BEGIN CERTIFICATE-----
MIIETTCCAzWgAwIBAgILBAAAAAABRE7wNjEwDQYJKoZIhvcNAQELBQAwVzELMAkG
A1UEBhMCQkUxGTAXBgNVBAoTEEdsb2JhbFNpZ24gbnYtc2ExEDAOBgNVBAsTB1Jv
b3QgQ0ExGzAZBgNVBAMTEkdsb2JhbFNpZ24gUm9vdCBDQTAeFw0xNDAyMjAxMDAw
MDBaFw0yNDAyMjAxMDAwMDBaMEwxCzAJBgNVBAYTAkJFMRkwFwYDVQQKExBHbG9i
YWxTaWduIG52LXNhMSIwIAYDVQQDExlBbHBoYVNTTCBDQSAtIFNIQTI1NiAtIEcy
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA2gHs5OxzYPt+j2q3xhfj
kmQy1KwA2aIPue3ua4qGypJn2XTXXUcCPI9A1p5tFM3D2ik5pw8FCmiiZhoexLKL
dljlq10dj0CzOYvvHoN9ItDjqQAu7FPPYhmFRChMwCfLew7sEGQAEKQFzKByvkFs
MVtI5LHsuSPrVU3QfWJKpbSlpFmFxSWRpv6mCZ8GEG2PgQxkQF5zAJrgLmWYVBAA
cJjI4e00X9icxw3A1iNZRfz+VXqG7pRgIvGu0eZVRvaZxRsIdF+ssGSEj4k4HKGn
kCFPAm694GFn1PhChw8K98kEbSqpL+9Cpd/do1PbmB6B+Zpye1reTz5/olig4het
ZwIDAQABo4IBIzCCAR8wDgYDVR0PAQH/BAQDAgEGMBIGA1UdEwEB/wQIMAYBAf8C
AQAwHQYDVR0OBBYEFPXN1TwIUPlqTzq3l9pWg+Zp0mj3MEUGA1UdIAQ+MDwwOgYE
VR0gADAyMDAGCCsGAQUFBwIBFiRodHRwczovL3d3dy5hbHBoYXNzbC5jb20vcmVw
b3NpdG9yeS8wMwYDVR0fBCwwKjAooCagJIYiaHR0cDovL2NybC5nbG9iYWxzaWdu
Lm5ldC9yb290LmNybDA9BggrBgEFBQcBAQQxMC8wLQYIKwYBBQUHMAGGIWh0dHA6
Ly9vY3NwLmdsb2JhbHNpZ24uY29tL3Jvb3RyMTAfBgNVHSMEGDAWgBRge2YaRQ2X
yolQL30EzTSo//z9SzANBgkqhkiG9w0BAQsFAAOCAQEAYEBoFkfnFo3bXKFWKsv0
XJuwHqJL9csCP/gLofKnQtS3TOvjZoDzJUN4LhsXVgdSGMvRqOzm+3M+pGKMgLTS
xRJzo9P6Aji+Yz2EuJnB8br3n8NA0VgYU8Fi3a8YQn80TsVD1XGwMADH45CuP1eG
l87qDBKOInDjZqdUfy4oy9RU0LMeYmcI+Sfhy+NmuCQbiWqJRGXy2UzSWByMTsCV
odTvZy84IOgu/5ZR8LrYPZJwR2UcnnNytGAMXOLRc3bgr07i5TelRS+KIz6HxzDm
MTh89N1SyvNTBCVXVmaU6Avu5gMUTu79bZRknl7OedSyps9AsUSoPocZXun4IRZZUw==
-----END CERTIFICATE-----
`

func createTunnel(raddr *net.TCPAddr) (*yamux.Session, error) {
	roots := x509.NewCertPool()
	ok := roots.AppendCertsFromPEM([]byte(ROOTCA))
	if !ok {
		log.Error("failed to parse root certificate")
	}
	config := tls.Config{
		// RootCAs: roots,
		// InsecureSkipVerify: DEBUG,
		ServerName:         "618033988.cc",
		ClientSessionCache: tls.NewLRUClientSessionCache(32), // use sessoin ticket to speed up tls handshake
	}
	conn, err := tls.Dial("tcp", raddr.String(), &config)
	if err != nil {
		return nil, err
	}
	// TODO: config client
	return yamux.Client(conn, nil)
}

func ip2int(ip net.IP) int {
	if ip == nil {
		return 0
	}
	ip = ip.To4()

	var ipInt int
	ipInt += int(ip[0]) << 24
	ipInt += int(ip[1]) << 16
	ipInt += int(ip[2]) << 8
	ipInt += int(ip[3])
	return ipInt
}
