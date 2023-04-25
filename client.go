package min_rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/wuxming/min/codec"
)

// Call 承载一次 RPC 调用的信息
type Call struct {
	Seq           uint64      //调用的唯一标识
	ServiceMethod string      // format "service.method"
	Args          interface{} //方法的参数
	Reply         interface{} //方法函数的返回值
	Error         error       //错误信息
	Done          chan *Call  //做异步处理的队列
}

// rcp 调用完成之后，将其放到队列，其后面可以选择异步接收
func (c *Call) done() {
	c.Done <- c
}

type Client struct {
	cc       codec.Codec //编解码器，序列化与反序列化
	opt      *Option
	sending  sync.Mutex       // 保证请求有序发送
	header   codec.Header     //请求头
	mu       sync.Mutex       //并发锁
	seq      uint64           //每个请求的序列号
	pending  map[uint64]*Call //存储未处理完,正在处理的请求
	closing  bool             //用户关闭，Client 处于不可用的状态
	shutdown bool             //错误发送，导致 Client 不可用
}

//--------------------------- 连接 rpc 服务器  ---------------------------------------------

// Dial 连接指定网络地址的 rpc 服务器
func Dial(network, addres string, opts ...*Option) (client *Client, err error) {
	//这个目的是 opt 可有可无，有则必须是一个
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial(network, addres)
	if err != nil {
		return nil, err
	}
	//关闭连接
	defer func() {
		if client == nil {
			_ = conn.Close()
		}
	}()
	return NewClient(conn, opt)
}

// 解析 options
func parseOptions(opts ...*Option) (*Option, error) {
	if len(opts) == 0 || opts[0] == nil {
		return DefaultOption, nil
	}
	if len(opts) != 1 {
		return nil, errors.New("number of option is more than 1")
	}
	opt := opts[0]
	opt.MagicNumber = DefaultOption.MagicNumber
	if opt.CodecType == "" {
		opt.CodecType = DefaultOption.CodecType
	}
	return opt, nil
}

//--------------------------- 客户端相关 ---------------------------------------------

// NewClient 生成客户端并建立编码方式
func NewClient(conn net.Conn, opt *Option) (*Client, error) {
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		//未实现的编解码方式
		err := fmt.Errorf("invalid codec type %s", opt.CodecType)
		log.Println("rpc client: codec error:", err)
		return nil, err
	}
	//发送编解码的方式给服务端
	err := json.NewEncoder(conn).Encode(opt)
	if err != nil {
		log.Println("rpc client: options error: ", err)
		_ = conn.Close()
		return nil, err
	}
	return newClientCodec(f(conn), opt), nil
}
func newClientCodec(cc codec.Codec, option *Option) *Client {
	cletnt := &Client{
		cc:      cc,
		opt:     option,
		seq:     1, //序列号从 1 开始
		pending: make(map[uint64]*Call),
	}
	//开启后台协程取处理返回的结果
	go cletnt.receive()
	return cletnt
}

var _ io.Closer = (*Client)(nil)

var ErrrShutdown = errors.New("connection is shut down")

// Close 关闭连接
func (c *Client) Close() error {
	c.mu.Lock()
	defer func() {
		c.mu.Unlock()
	}()
	if c.closing {
		return ErrrShutdown
	}
	c.closing = true
	return c.cc.Close()
}

// IsAvailable 获取客户端连接是否可用
func (c *Client) IsAvailable() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.shutdown && !c.closing
}

//--------------------------- 发送 rpc 请求  ---------------------------------------------

// Call 同步接口
func (c *Client) Call(serviceMethod string, args, reply interface{}) error {
	call := <-c.Go(serviceMethod, args, reply, make(chan *Call, 1)).Done
	return call.Error
}

// Go 异步接口
func (c *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		log.Panic("rpc client: done channel is unbuffered")
	}
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	c.send(call)
	return call
}

func (c *Client) send(call *Call) {
	// 并发处理
	c.sending.Lock()
	defer c.sending.Unlock()
	//调用注册
	seq, err := c.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}
	c.header.ServiceMethod = call.ServiceMethod
	c.header.Seq = seq
	c.header.Error = ""
	if err = c.cc.Write(&(c.header), call.Args); err != nil {
		//写入失败，移出pending map，放到异步调用队列
		callTorm := c.removeCall(seq)
		if callTorm != nil {
			callTorm.Error = err
			callTorm.done()
		}
	}
}

//--------------------------- rpc 响应处理 ---------------------------------------------

// receive 接收响应，读取响应信息
func (c *Client) receive() {
	var err error
	for err == nil {
		var h codec.Header
		//这里主要是读取 error
		if err = c.cc.ReadHeader(&h); err != nil {
			break
		}
		//将此次 call 移出 pending，读取信息
		call := c.removeCall(h.Seq)
		switch {
		case call == nil:
			//表示写入部分失败
			err = c.cc.ReadBody(nil)
		case h.Error != "":
			{
				call.Error = fmt.Errorf(h.Error)
				err = c.cc.ReadBody(nil)
				call.done()
			}
		default:
			err = c.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body " + err.Error())
			}
			call.done()
		}
	}
	//中止其他相应信息的处理
	c.terminateCalls(err)
}

//--------------------------- Call处理相关 ---------------------------------------------

// registerCall 请求注册
func (c *Client) registerCall(call *Call) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.shutdown || c.closing {
		return 0, ErrrShutdown
	}
	call.Seq = c.seq
	c.pending[call.Seq] = call
	c.seq++ //序列号递增
	return call.Seq, nil
}

// removeCall 将请求移出正在处理的 map 中，读取信息
func (c *Client) removeCall(seq uint64) *Call {
	c.mu.Lock()
	defer c.mu.Unlock()
	call := c.pending[seq]
	delete(c.pending, seq)
	return call
}

// terminateCalls 中止请求
func (c *Client) terminateCalls(err error) {
	//两个锁 ？
	c.sending.Lock()
	defer c.sending.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.shutdown = true
	for _, call := range c.pending {
		call.Error = err
		call.done()
	}
}
