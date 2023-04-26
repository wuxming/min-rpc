package min_rpc

import (
	"fmt"
	"reflect"
	"testing"
)

type Foo int

type Args struct {
	num1, num2 int
}

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.num1 + args.num2
	return nil
}

func (f Foo) sum(args Args, reply *int) error {
	*reply = args.num1 + args.num2
	return nil
}
func _assert(condition bool, msg string, v ...interface{}) {
	if !condition {
		panic(fmt.Sprintf("assertion failed: "+msg, v...))
	}
}
func TestNewServer(t *testing.T) {
	var foo Foo
	s := newService(&foo)
	_assert(len(s.method) == 1, "wrong service Method, expect 1, but got %d", len(s.method))
	mType := s.method["Sum"]
	_assert(mType != nil, "wrong Method, Sum shouldn't nil")
}
func TestMethodType_Call(t *testing.T) {
	var foo Foo
	s := newService(&foo)
	mType := s.method["Sum"]
	argv := mType.newArgv()
	replyv := mType.newReplyv()
	//如果方法入参为指针，则应该 argv.Elem().Set()
	argv.Set(reflect.ValueOf(Args{
		num1: 1,
		num2: 2,
	}))
	err := s.call(mType, argv, replyv)
	_assert(err == nil && *replyv.Interface().(*int) == 3 && mType.NumCalls() == 1, "failed to call Foo.Sum")
}
