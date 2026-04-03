package clientside

import (
	"context"
	"fmt"

	"github.com/centrifugal/centrifuge-go"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"

	rpcframeworkprotocol "yao.io/grpc-over-websocket/rpcframework/protocol"
)

func NewConn(c *centrifuge.Client) grpc.ClientConnInterface {
	return &conn{client: c}
}

type conn struct {
	client *centrifuge.Client
	codec  rpcframeworkprotocol.ProtobufCodec
}

// Invoke performs a unary RPC and returns after the response is received into reply.
func (c *conn) Invoke(ctx context.Context, method string, args any, reply any, opts ...grpc.CallOption) error {
	in, ok := args.(protoreflect.ProtoMessage)
	if !ok {
		return fmt.Errorf("got unexpected args type: %T", args)
	}
	data, err := c.codec.Encode(in)
	if err != nil {
		return err
	}
	r, err := c.client.RPC(ctx, method, data)
	if err != nil {
		return err
	}
	out, ok := reply.(protoreflect.ProtoMessage)
	if !ok {
		return fmt.Errorf("got unexpected reply type: %T", reply)
	}
	return c.codec.Decode(r.Data, out)
}

// NewStream begins a streaming RPC.
func (c *conn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, fmt.Errorf("NewStream not implemented")
}
