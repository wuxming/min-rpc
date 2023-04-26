package min_rpc

import (
	"log"
	"net"
	"sync"
	"testing"
)

type Fo int
type Arg struct {
	Num1 int
	Num2 int
}

func (f Fo) Sum(arg Arg, relpy *int) error {
	*relpy = arg.Num1 + arg.Num2
	return nil
}
func startServe(addr chan string) {
	var fo Fo
	//服务注册
	if err := Register(&fo); err != nil {
		log.Fatal("register error:", err)
	}
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String()
	Accept(l)
}
func TestServer_ServeConn(t *testing.T) {
	log.SetFlags(0)
	addr := make(chan string)
	go startServe(addr)
	client, _ := Dial("tcp", <-addr)
	defer func() {
		_ = client.Close()
	}()
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := &Arg{
				Num1: i,
				Num2: i,
			}
			var reply int
			err := client.Call("Fo.Sum", args, &reply)
			if err != nil {
				log.Fatal("调用Fo.Sum失败 ", err.Error())
			}
			log.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
		}(i)
	}
	wg.Wait()
}
