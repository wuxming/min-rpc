package min_rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/wuxming/min/codec"
)

const MagicNumber = 0x3b5f5c

// Option 编解码方式
type Option struct {
	MagicNumber    int //秘钥，标记这是
	CodecType      codec.Type
	ConnectTimeout time.Duration //连接超市
	HandleTimeout  time.Duration //服务端处理超市
}

var DefaultOption = &Option{
	MagicNumber:    MagicNumber,
	CodecType:      codec.GobType,
	ConnectTimeout: time.Second * 10,
}

type Server struct {
	serviceMap sync.Map
}

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
func Register(rcvr interface{}) error {
	return DefaultServer.Register(rcvr)
}
func (s *Server) Register(rcvr interface{}) error {
	svc := newService(rcvr)
	if _, dup := s.serviceMap.LoadOrStore(svc.name, svc); dup {
		return errors.New("rpc：service already defined：" + svc.name)
	}
	return nil
}

func (s *Server) findService(serviceMethod string) (svc *service, mtype *methodType, err error) {
	//逗号分割
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server:service/method request 不符合规范 " + serviceMethod)
	}
	servicName, methodname := serviceMethod[:dot], serviceMethod[dot+1:]
	//加载服务
	svci, ok := s.serviceMap.Load(servicName)
	if !ok {
		err = errors.New("rpc server: 未找到服务 " + servicName)
	}
	svc = svci.(*service)
	//加载方法
	mtype = svc.method[methodname]
	if mtype == nil {
		err = errors.New("rpc server: 未找到方法 " + servicName)
	}
	return
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
		go s.handleRequest(cc, req, sending, wg, time.Second*10)
	}

	//连接报错，退出循环，等待协中的其他任务完成
	wg.Wait()

}

// request 封装请求
type request struct {
	h            *codec.Header //请求头
	argv, replyv reflect.Value //请求的命令和回复
	mtype        *methodType
	svc          *service
}

// readRequset 读取请求
func (s *Server) readRequset(cc codec.Codec) (*request, error) {
	h, err := s.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	req := &request{h: h}
	//查询服务对应的方法
	req.svc, req.mtype, err = s.findService(h.ServiceMethod)
	if err != nil {
		return req, err
	}

	req.argv = req.mtype.newArgv()
	req.replyv = req.mtype.newReplyv()
	argvi := req.argv.Interface()
	//确保 argvi 是指针
	if req.argv.Type().Kind() != reflect.Ptr {
		argvi = req.argv.Addr().Interface()
	}
	if err = cc.ReadBody(argvi); err != nil {
		log.Println("rpc server: read argv err:", err)
		return req, err
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
	// todo 错误的服务读了两遍，并且会报这个错误,待解决
	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error！！！！！！:", err)
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
func (s *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()
	called := make(chan struct{})
	sent := make(chan struct{})
	go func() {
		err := req.svc.call(req.mtype, req.argv, req.replyv)
		called <- struct{}{}
		if err != nil {
			req.h.Error = err.Error()
			s.sendResponse(cc, req.h, errorRequest, sending)
			sent <- struct{}{}
			return
		}
		s.sendResponse(cc, req.h, req.replyv.Interface(), sending)
		sent <- struct{}{}
	}()
	if timeout == 0 {
		<-called
		<-sent
		return
	}
	select {
	case <-time.After(timeout):
		req.h.Error = fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout)
		s.sendResponse(cc, req.h, errorRequest, sending)
	case <-called:
		<-sent
	}
}
