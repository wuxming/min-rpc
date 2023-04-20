package min_rpc

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"sync"

	"github.com/wuxming/min/codec"
)

const MagicNumber = 0x3b5f5c

// Option 编解码方式
type Option struct {
	MagicNumber int //秘钥，标记这是
	CodecType   codec.Type
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType,
}

type Server struct{}

func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

// Accept 默认的接收
func Accept(lis net.Listener) {
	DefaultServer.Accept(lis)
}

// Accept  等待连接
func (s *Server) Accept(lis net.Listener) {
	for {
		coon, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error:", err)
			return
		}
		//连接成功,开协程非阻塞处理连接
		go s.ServeConn(coon)
	}
}

// ServeConn 处理连接
func (s *Server) ServeConn(conn io.ReadWriteCloser) {
	//读取编码方式
	var opt Option
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: options error: ", err)
		return
	}
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x", opt.MagicNumber)
	}
	// 获取编解码器的函数
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		log.Printf("rpc server: invalid codec type %s", opt.CodecType)
		return
	}
	//处理 Option 后面的数据流
	s.serveCodec(f(conn))
}

// errorRequest 空结构体占位错误请求体
var errorRequest = struct{}{}

// 服务处理
func (s *Server) serveCodec(cc codec.Codec) {
	defer func() {
		_ = cc.Close()
	}()
	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	for {
		//不断的读取请求
		req, err := s.readRequset(cc)
		if err != nil {
			if req == nil {
				break
			}
			req.h.Error = err.Error()
			//发送错误相应
			s.sendResponse(cc, req.h, errorRequest, sending)
			continue
		}
		wg.Add(1)
		go s.handleRequest(cc, req, sending, wg)
	}

	//连接报错，退出循环，等待协中的其他任务完成
	wg.Wait()

}

// request 封装请求
type request struct {
	h            *codec.Header //请求头
	argv, replyv reflect.Value //请求的命令和回复
}

// readRequset 读取请求
func (s *Server) readRequset(cc codec.Codec) (*request, error) {
	h, err := s.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	req := &request{h: h}
	//晦涩难懂，
	req.argv = reflect.New(reflect.TypeOf("")) //返回字符串零值 指针类型的反射对象
	//转换为指针实例赋值
	if err := cc.ReadBody(req.argv.Interface()); err != nil {
		log.Println("rpc server: read argv err:", err)
	}
	// reflect.New(reflect.TypeOf(""))
	//      ||
	// s := "";
	// reflect.ValueOf(&s)
	// req.argv.Interface() = &s
	return req, nil
}

// readRequestHeader 读取请求头
func (s *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &h, nil
}

// sendResponse 发送响应，读取请求是并发的，但响应是逐一的， 并发会导致客户端无法解析，所以要用锁。
func (s *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}
func (s *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
	defer wg.Done()
	log.Println(req.h, req.argv.Elem())
	req.replyv = reflect.ValueOf(fmt.Sprintf("resp %d", req.h.Seq))
	s.sendResponse(cc, req.h, req.replyv.Interface(), sending)
}
