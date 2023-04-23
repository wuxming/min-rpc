package min_rpc

import (
	"log"
	"sync"
	"testing"
)

func TestClient(t *testing.T) {
	addr := make(chan string)
	go startServe(addr)
	c, _ := Dial("tcp", <-addr)
	for i := 0; i < 5; i++ {
		var reply string
		err := c.Call("FOO.sum", "test", &reply)
		if err != nil {
			log.Println(err)
		}
		log.Println("reply:", reply)
	}

}
func TestClient_sync(t *testing.T) {
	addr := make(chan string)
	go startServe(addr)
	c, _ := Dial("tcp", <-addr)
	var reply string
	call := c.Go("FOO.sum", "test", &reply, nil)
	wg := sync.WaitGroup{}
	wg.Add(1)
	//异步处理
	go func(call *Call) {
		defer func() {
			wg.Done()
		}()
		<-call.Done
		log.Println("reply:", reply)
	}(call)
	// 去做其他程序的处理,
	wg.Wait()

}
