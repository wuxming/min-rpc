package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

// GobCodec gob 编解码实例，实现 codec 接口
type GobCodec struct {
	//tcp连接实例
	conn io.ReadWriteCloser
	//写入缓冲区，提升性能
	buf *bufio.Writer
	// gob 解码实体、反序列化
	dec *gob.Decoder
	// gob 编码实体、序列化
	enc *gob.Encoder
}

func (g *GobCodec) Close() error {
	return g.conn.Close()
}

// ReadHeader 数据读取解码给 header
func (g *GobCodec) ReadHeader(header *Header) error {
	return g.dec.Decode(header)
}

// ReadBody 数据读取解码给 body，body 要传入指针
func (g *GobCodec) ReadBody(body interface{}) error {
	return g.dec.Decode(body)
}

func (g *GobCodec) Write(header *Header, body interface{}) (err error) {
	defer func() {
		//将写缓冲区数据刷新，写入到底层 conn 连接中
		_ = g.buf.Flush()
		if err != nil {
			_ = g.Close()
		}

	}()
	if err = g.enc.Encode(header); err != nil {
		log.Println("rpc codec: gob error encoding header:", err)
		return err
	}
	if err = g.enc.Encode(body); err != nil {
		log.Println("rpc codec: gob error encoding body:", err)
		return err
	}
	return nil
}

//将 nil 转换为 GobCodec 结构体 ，并赋值给 Codec 接口，
//来证明 GobCodec 实现了 Codec
var _ Codec = (*GobCodec)(nil)

func NewGobCodec(conn io.ReadWriteCloser) Codec {
	buf := bufio.NewWriter(conn)
	return &GobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn),
		enc:  gob.NewEncoder(buf),
	}
}
