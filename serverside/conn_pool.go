package serverside

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
)

var (
	ErrUserIDMissing    = errors.New("user id is missing in context")
	ErrConnLostOrClosed = errors.New("client connection is lost or closed")
)

var logger = grpclog.Component("clientside")

func newConnPool() *ConnPool {
	return &ConnPool{
		clientIDToConn:    make(map[string]*conn),
		userIDToClientIDs: make(map[string]stringSet),
	}
}

type stringSet map[string]struct{}

type ConnPool struct {
	mu sync.RWMutex

	clientIDToConn    map[string]*conn
	userIDToClientIDs map[string]stringSet
}

func (pool *ConnPool) addConn(c *conn) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	clientID := c.client.ID()
	userID := c.client.UserID()
	pool.clientIDToConn[clientID] = c

	clientIDs, ok := pool.userIDToClientIDs[userID]
	if !ok {
		clientIDs = make(stringSet)
		pool.userIDToClientIDs[userID] = clientIDs
	}
	clientIDs[clientID] = struct{}{}
}

func (pool *ConnPool) removeConnAndClose(clientID string) (*conn, bool) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	c, ok := pool.clientIDToConn[clientID]
	if !ok {
		return nil, false
	}
	close(c.closeCh)

	userID := c.client.UserID()
	clientIDs, ok := pool.userIDToClientIDs[userID]
	if ok {
		delete(clientIDs, clientID)
		if len(clientIDs) == 0 {
			delete(pool.userIDToClientIDs, userID)
		}
	}
	return c, true
}

func (pool *ConnPool) GetConnByUserID(userID string) (*conn, bool) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	clientIDs, ok := pool.userIDToClientIDs[userID]
	if ok {
		if len(clientIDs) > 1 {
			logger.Warningf("got multiple conns for user id: %s", userID)
		}
		for clientID := range clientIDs {
			c, ok := pool.clientIDToConn[clientID]
			if ok {
				return c, true
			}
		}
	}
	return nil, false
}

func (pool *ConnPool) Invoke(ctx context.Context, method string, args any, reply any, opts ...grpc.CallOption) error {
	userID, ok := UserIDFrom(ctx)
	if !ok {
		return ErrUserIDMissing
	}
	conn, ok := pool.GetConnByUserID(userID)
	if !ok {
		return ErrConnLostOrClosed
	}
	return conn.Invoke(ctx, method, args, reply, opts...)
}

func (pool *ConnPool) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, fmt.Errorf("NewStream not implemented")
}

type userKey struct{}

func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userKey{}, userID)
}

func UserIDFrom(ctx context.Context) (string, bool) {
	val, ok := ctx.Value(userKey{}).(string)
	return val, ok
}
