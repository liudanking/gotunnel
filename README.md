# gotunnel
A light weight Multiplexing TCP tunnel written in Go.


# Roadmap
* multiplexing: {stream num, {header, payload}} {2, {2, payload}}
* cipher: 


# TODO
* struct Frame --> []byte Frame
* custom memory pool. Ref goim libs/bytes/buffer.go
* buffered channel for frame


# Credits

* [goim](https://github.com/Terry-Mao/goim)