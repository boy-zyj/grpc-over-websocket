package clientside

import (
	"context"
	"fmt"
	"strings"

	"github.com/centrifugal/centrifuge-go"
	"github.com/centrifugal/protocol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"

	rpcframeworkprotocol "yao.io/grpc-over-websocket/rpcframework/protocol"
	"yao.io/grpc-over-websocket/rpcframework/registrar"
)

var logger = grpclog.Component("clientside")

type GRPCGateway struct {
	codec     rpcframeworkprotocol.ProtobufCodec
	client    *centrifuge.Client
	registrar *registrar.GRPCServiceRegistrar
}

func NewGRPCGateway(c *centrifuge.Client) (*GRPCGateway, error) {
	r := registrar.NewGRPCServiceRegistrar()
	gw := &GRPCGateway{
		client:    c,
		registrar: r,
	}
	gw.registerEventHandlers()
	return gw, nil
}

func (gw *GRPCGateway) registerEventHandlers() {
	gw.client.OnConnected(func(ce centrifuge.ConnectedEvent) {
		logger.Infof("connected, client id: %s", ce.ClientID)
	})

	gw.client.OnPublication(func(pub centrifuge.ServerPublicationEvent) {
		if clientID, ok := isServersideRpcCallChannel(pub.Channel); ok {
			var rpcCall protocol.Command
			err := gw.codec.Decode(pub.Data, &rpcCall)
			if err != nil {
				logger.Errorf("failed to decode server-side rpc call: %v", err)
				return
			}
			if rpcCall.Id == 0 || rpcCall.Rpc == nil {
				logger.Errorf(".id or .rpc missing in server-side rpc call")
				return
			}
			id := rpcCall.Id
			rpc := rpcCall.Rpc

			ctx := context.TODO()
			var cb = func(clientID string, reply *protocol.Reply) {
				data, err := gw.codec.Encode(reply)
				if err != nil {
					logger.Errorf("failed to encode %T: %v", reply, err)
					return
				}
				_, err = gw.client.RPC(ctx, rpcframeworkprotocol.MethodPrefixClientsideRpcReply+clientID, data)
				if err != nil {
					logger.Errorf("failed to send back rpc reply: %v", err)
				}
			}

			go func() {
				var dec = func(in any) error {
					return gw.codec.Decode(rpc.Data, in.(protoreflect.ProtoMessage))
				}
				out, err := gw.registrar.CallGRPC(ctx, rpc.Method, dec)
				if err != nil {
					s, _ := status.FromError(err)
					cb(clientID, &protocol.Reply{Id: id, Error: &protocol.Error{Code: uint32(s.Code()), Message: s.Message()}})
					return
				}
				data, err := gw.codec.Encode(out.(protoreflect.ProtoMessage))
				if err != nil {
					cb(clientID, &protocol.Reply{Id: id, Error: &protocol.Error{Code: uint32(codes.Internal), Message: fmt.Sprintf("failed to encode response: %v", err)}})
					return
				}
				cb(clientID, &protocol.Reply{Id: id, Rpc: &protocol.RPCResult{Data: protocol.Raw(data)}})
			}()
		}
	})
}

func (gw *GRPCGateway) RegisterService(sd *grpc.ServiceDesc, ss interface{}) {
	gw.registrar.RegisterService(sd, ss)
}

func isServersideRpcCallChannel(channel string) (string, bool) {
	if strings.HasPrefix(channel, rpcframeworkprotocol.ChannelPrefixServersideRPCCall) {
		return channel[len(rpcframeworkprotocol.ChannelPrefixServersideRPCCall):], true
	}
	return "", false
}
