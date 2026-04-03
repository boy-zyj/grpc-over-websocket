package serverside

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/centrifugal/centrifuge"
	"github.com/centrifugal/protocol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"

	rpcframeworkprotocol "yao.io/grpc-over-websocket/rpcframework/protocol"
)

var (
	ErrTimeout = errors.New("timeout")
)

type conn struct {
	mu       sync.RWMutex
	cmdID    uint32
	closeCh  chan struct{}
	codec    rpcframeworkprotocol.ProtobufCodec
	node     *centrifuge.Node
	client   *centrifuge.Client
	requests map[uint32]request
}

func (c *conn) Invoke(ctx context.Context, method string, args any, reply any, opts ...grpc.CallOption) error {
	data, err := c.codec.Encode(args.(protoreflect.ProtoMessage))
	if err != nil {
		return err
	}

	resCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	c.sendRPC(ctx, method, data, func(result []byte, err error) {
		resCh <- result
		errCh <- err
	})

	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-resCh:
		err = <-errCh
		if err != nil {
			return err
		}
		return c.codec.Decode(res, reply.(protoreflect.ProtoMessage))
	}
}

func (c *conn) nextCmdID() uint32 {
	return atomic.AddUint32(&c.cmdID, 1)
}

func (c *conn) sendRPC(ctx context.Context, method string, data []byte, fn func([]byte, error)) {
	select {
	case <-ctx.Done():
		fn(nil, ctx.Err())
		return
	default:
	}
	cmd := &protocol.Command{
		Id: c.nextCmdID(),
	}

	params := &protocol.RPCRequest{
		Data:   data,
		Method: method,
	}

	cmd.Rpc = params

	err := c.sendAsync(cmd, func(r *protocol.Reply, err error) {
		if err != nil {
			fn(nil, err)
			return
		}
		if r.Error != nil {
			fn(nil, errorFromProto(r.Error))
			return
		}
		fn(r.Rpc.Data, nil)
	})
	if err != nil {
		fn(nil, err)
		return
	}
}

func (c *conn) sendAsync(cmd *protocol.Command, cb func(*protocol.Reply, error)) error {
	c.addRequest(cmd.Id, cb)

	err := c.send(cmd)
	if err != nil {
		c.removeRequest(cmd.Id)
		return err
	}
	go func() {
		select {
		case <-time.After(time.Second * 30): // todo: timeout
			req, ok := c.popRequest(cmd.Id)
			if !ok {
				return
			}
			req.cb(nil, ErrTimeout)
		case <-c.closeCh:
			req, ok := c.popRequest(cmd.Id)
			if !ok {
				return
			}
			req.cb(nil, io.EOF)
		}
	}()
	return nil
}

func (c *conn) send(cmd *protocol.Command) error {
	data, err := c.codec.Encode(cmd)
	if err != nil {
		return err
	}
	channel := rpcframeworkprotocol.ChannelPrefixServersideRPCCall + c.client.ID()
	_, err = c.node.Publish(channel, data)
	return err
}

type request struct {
	cb func(*protocol.Reply, error)
}

func (c *conn) addRequest(id uint32, cb func(*protocol.Reply, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests[id] = request{cb}
}

func (c *conn) removeRequest(id uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.requests, id)
}

func (c *conn) popRequest(id uint32) (request, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	req, ok := c.requests[id]
	if ok {
		delete(c.requests, id)
	}
	return req, ok
}

func (c *conn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errors.New("NewStream not implemented")
}

func errorFromProto(err *protocol.Error) error {
	return status.Error(codes.Code(err.Code), err.Message)
}
