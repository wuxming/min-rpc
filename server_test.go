package min_rpc

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"testing"

	"github.com/wuxming/min/codec"
)

func startServe(addr chan string) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String()
	Accept(l)
}
func TestServer_ServeConn(t *testing.T) {
	addr := make(chan string)
	go startServe(addr)
	conn, _ := net.Dial("tcp", <-addr)
	//发送 option
	_ = json.NewEncoder(conn).Encode(DefaultOption)
	cc := codec.NewGobCodec(conn)
	for i := 0; i < 5; i++ {
		h := &codec.Header{
			ServiceMethod: "FOO.sum",
			Seq:           uint64(i),
		}
		_ = cc.Write(h, fmt.Sprintf("geerpc req %d", h.Seq))
		_ = cc.ReadHeader(h)
		var reply string
		_ = cc.ReadBody(&reply)
		log.Println("reply:", reply)
	}
}
