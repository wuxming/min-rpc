package codec

import "io"

type Header struct {
	//服务名和方法名，Service.Method
	ServiceMethod string
	//请求的序号，用来区分不同的请求
	Seq uint64
	//错误信息
	Error string
}

// Codec 对消息进行编解码的接口
type Codec interface {
	io.Closer
	ReadHeader(*Header) error
	ReadBody(interface{}) error
	Write(*Header, interface{}) error
}

// NewCodeFunc 自定义函数，生成Codec
type NewCodeFunc func(io.ReadWriteCloser) Codec

// Type 自定义编码类型
type Type string

const (
	GobType  Type = "application/gob"
	JsonType Type = "application/json"
)

// NewCodecFuncMap 编解码的 map 根据不同的 Type 存储对应的生成函数
var NewCodecFuncMap map[Type]NewCodeFunc

func init() {
	NewCodecFuncMap = make(map[Type]NewCodeFunc)
	NewCodecFuncMap[GobType] = NewGobCodec
}
