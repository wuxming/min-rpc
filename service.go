package min_rpc

import (
	"go/ast"
	"log"
	"reflect"
	"sync/atomic"
)

type methodType struct {
	method    reflect.Method //方法本身
	ArgType   reflect.Type   //第一个参数，参数
	ReplyType reflect.Type   //第二个参数，返回值
	numCalls  uint64         //调用的次数
}

func (m *methodType) NumCalls() uint64 {
	//获取值，不受其他影响
	return atomic.LoadUint64(&m.numCalls)
}
func (m *methodType) newArgv() reflect.Value {
	var argv reflect.Value
	//arg 可能是指针也可能值
	if m.ArgType.Kind() == reflect.Ptr {
		//取值，生成指针 反射对象
		argv = reflect.New(m.ArgType.Elem())
	} else {
		//生成指针 反射对象，取值
		argv = reflect.New(m.ArgType).Elem()
	}
	return argv
}
func (m *methodType) newReplyv() reflect.Value {
	// 返回值一定是指针
	replyv := reflect.New(m.ReplyType.Elem())
	// map 与 slice 用不同的方式初始化
	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(m.ReplyType, 0, 0))
	}
	return replyv
}

type service struct {
	name   string                 //映射结构体名称
	typ    reflect.Type           // 结构体类型
	rcvr   reflect.Value          //结构体实例本身
	method map[string]*methodType //存储映射的结构体的所有符合条件的方法
}

func newService(rcvr interface{}) *service {
	s := new(service)
	s.rcvr = reflect.ValueOf(rcvr)
	//如果是指针的话 Indirect 值
	s.name = reflect.Indirect(s.rcvr).Type().Name()
	s.typ = reflect.TypeOf(rcvr)
	if !ast.IsExported(s.name) {
		log.Fatalf("rpc server: %s is not a valid service name", s.name)
	}
	s.registerMethods()
	return s
}
func (s *service) registerMethods() {
	s.method = make(map[string]*methodType)
	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mType := method.Type
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}
		//返回值要是 error
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}
		//第一个参数是其服务本身
		argType, replyType := mType.In(1), mType.In(2)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}
		s.method[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}
}

// isExportedOrBuiltinType 判断是否是 暴露或则是内置的
func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}
func (s *service) call(m *methodType, argv, replyv reflect.Value) error {
	//保证并发，数据一致性，调用次数+1
	atomic.AddUint64(&m.numCalls, 1)
	f := m.method.Func
	returnValues := f.Call([]reflect.Value{s.rcvr, argv, replyv})
	if errInter := returnValues[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}
