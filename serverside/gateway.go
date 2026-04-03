package serverside

import (
	"context"
	"net/http"
	"strings"

	"github.com/centrifugal/centrifuge"
	"github.com/centrifugal/protocol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"

	rpcframeworkprotocol "yao.io/grpc-over-websocket/rpcframework/protocol"
	"yao.io/grpc-over-websocket/rpcframework/registrar"
)

func NewGRPCGateway(authenticator Authenticator) (*GRPCGateway, error) {
	node, err := centrifuge.New(centrifuge.Config{
		LogLevel:   centrifuge.LogLevelDebug,
		LogHandler: handleLog,
	})
	if err != nil {
		return nil, err
	}

	r := registrar.NewGRPCServiceRegistrar()
	gw := &GRPCGateway{
		node:          node,
		registrar:     r,
		connPool:      newConnPool(),
		authenticator: authenticator,
	}
	gw.registerEventHandlers()
	return gw, nil
}

type GRPCGateway struct {
	codec         rpcframeworkprotocol.ProtobufCodec
	node          *centrifuge.Node
	registrar     *registrar.GRPCServiceRegistrar
	connPool      *ConnPool
	authenticator Authenticator
}

func (gw *GRPCGateway) registerEventHandlers() {
	gw.node.OnConnecting(func(ctx context.Context, e centrifuge.ConnectEvent) (centrifuge.ConnectReply, error) {
		cred, data, allowed, err := gw.authenticator.Authenticate(ctx, e.Token, e.Data)
		if err != nil {
			return centrifuge.ConnectReply{}, centrifuge.ErrorInternal
		}
		if !allowed {
			return centrifuge.ConnectReply{}, centrifuge.ErrorUnauthorized
		}
		return centrifuge.ConnectReply{
			Credentials: cred,
			Data:        data,
			// Subscribe to a personal server-side channel.
			Subscriptions: map[string]centrifuge.SubscribeOptions{
				rpcframeworkprotocol.ChannelPrefixServersideRPCCall + e.ClientID: {
					EnableRecovery: false,
					EmitPresence:   true,
					EmitJoinLeave:  true,
					PushJoinLeave:  true,
				},
			},
		}, nil
	})

	gw.node.OnConnect(func(client *centrifuge.Client) {
		var c *conn = &conn{
			closeCh:  make(chan struct{}),
			codec:    gw.codec,
			node:     gw.node,
			client:   client,
			requests: make(map[uint32]request),
		}

		gw.connPool.addConn(c)

		client.OnDisconnect(func(e centrifuge.DisconnectEvent) {
			logger.Errorf("client %s disconnected: %v", client.ID(), e)
			gw.connPool.removeConnAndClose(client.ID())
		})

		client.OnRPC(func(e centrifuge.RPCEvent, cb centrifuge.RPCCallback) {
			if clientID, ok := isClientsideRpcReplyMethod(e.Method); ok {
				cb(centrifuge.RPCReply{}, nil)
				if clientID != client.ID() {
					logger.Errorf("got mismatched peer client id: %s", clientID)
					return
				}
				var reply protocol.Reply
				err := gw.codec.Decode(e.Data, &reply)
				if err != nil {
					logger.Errorf("failed to decode client-side rpc reply: %v", err)
					return
				}
				if reply.Error == nil && reply.Rpc == nil {
					logger.Errorf("got invalid rpc reply from client-side")
					return
				}

				req, ok := c.popRequest(reply.Id)
				if !ok {
					return
				}
				req.cb(&reply, nil)
				return
			}

			go func() {
				ctx := context.TODO()
				var dec = func(in any) error {
					return gw.codec.Decode(e.Data, in.(protoreflect.ProtoMessage))
				}
				reply, err := gw.registrar.CallGRPC(ctx, e.Method, dec)
				if err != nil {
					s, _ := status.FromError(err)
					cb(centrifuge.RPCReply{}, &centrifuge.Error{Code: uint32(s.Code()), Message: s.Message()})
					return
				}
				data, err := gw.codec.Encode(reply.(protoreflect.ProtoMessage))
				if err != nil {
					cb(centrifuge.RPCReply{}, centrifuge.ErrorInternal)
					return
				}
				cb(centrifuge.RPCReply{Data: data}, nil)
			}()
		})
	})
}

func (gw *GRPCGateway) RegisterService(sd *grpc.ServiceDesc, ss interface{}) {
	gw.registrar.RegisterService(sd, ss)
}

func (gw *GRPCGateway) ConnPool() *ConnPool {
	return gw.connPool
}

func (gw *GRPCGateway) NewHTTPHandler() http.Handler {
	return centrifuge.NewWebsocketHandler(
		gw.node,
		centrifuge.WebsocketConfig{
			MessageSizeLimit:   1024 * 1024, // 1MB
			UseWriteBufferPool: true,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	)
}

func (gw *GRPCGateway) StartServing() error {
	return gw.node.Run()
}

func handleLog(e centrifuge.LogEntry) {
	logger.Infof("%s: %v", e.Message, e.Fields)
}

func isClientsideRpcReplyMethod(method string) (string, bool) {
	if strings.HasPrefix(method, rpcframeworkprotocol.MethodPrefixClientsideRpcReply) {
		return method[len(rpcframeworkprotocol.MethodPrefixClientsideRpcReply):], true
	}
	return "", false
}
