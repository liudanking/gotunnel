# gotunnel
A light weight Multiplexing TCP tunnel written in Go.


# Roadmap
* multiplexing: {stream id, cmd, length, payload}
* cipher: default TLS with session ticket

# Bug
* EOF not working
* history frame stucks new tunnel

# TODO
* ~~struct Frame --> []byte Frame~~
* ~~custom memory pool. Ref goim libs/bytes/buffer.go~~
* buffered channel for frame
* multi tunnel

# Credits

* [goim](https://github.com/Terry-Mao/goim)