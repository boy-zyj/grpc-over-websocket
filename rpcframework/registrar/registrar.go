/*
 *
 * Copyright 2014 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package registrar

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/status"
)

var logger = grpclog.Component("registrar")

// A ServerOption sets options such as credentials, codec and keepalive parameters, etc.
type ServerOption interface {
	apply(*serverOptions)
}

// funcServerOption wraps a function that modifies serverOptions into an
// implementation of the ServerOption interface.
type funcServerOption struct {
	f func(*serverOptions)
}

func (fdo *funcServerOption) apply(do *serverOptions) {
	fdo.f(do)
}

func newFuncServerOption(f func(*serverOptions)) *funcServerOption {
	return &funcServerOption{
		f: f,
	}
}

// UnaryInterceptor returns a ServerOption that sets the UnaryServerInterceptor for the
// server. Only one unary interceptor can be installed. The construction of multiple
// interceptors (e.g., chaining) can be implemented at the caller.
func UnaryInterceptor(i grpc.UnaryServerInterceptor) ServerOption {
	return newFuncServerOption(func(o *serverOptions) {
		if o.unaryInt != nil {
			panic("The unary server interceptor was already set and may not be reset.")
		}
		o.unaryInt = i
	})
}

// ChainUnaryInterceptor returns a ServerOption that specifies the chained interceptor
// for unary RPCs. The first interceptor will be the outer most,
// while the last interceptor will be the inner most wrapper around the real call.
// All unary interceptors added by this method will be chained.
func ChainUnaryInterceptor(interceptors ...grpc.UnaryServerInterceptor) ServerOption {
	return newFuncServerOption(func(o *serverOptions) {
		o.chainUnaryInts = append(o.chainUnaryInts, interceptors...)
	})
}

// GRPCServiceRegistrar 实现grpc.ServiceRegistrar接口
type GRPCServiceRegistrar struct {
	opts     serverOptions
	mu       sync.Mutex              // guards following
	services map[string]*serviceInfo // service name -> service info
}

// serviceInfo wraps information about a service. It is very similar to
// ServiceDesc and is constructed from it for internal purposes.
type serviceInfo struct {
	// Contains the implementation for the methods in this service.
	serviceImpl any
	methods     map[string]*grpc.MethodDesc
	mdata       any
}

// NewGRPCServiceRegistrar 创建一个新的动态服务注册器
func NewGRPCServiceRegistrar(opt ...ServerOption) *GRPCServiceRegistrar {
	opts := serverOptions{}
	for _, o := range opt {
		o.apply(&opts)
	}
	s := &GRPCServiceRegistrar{
		opts:     opts,
		services: make(map[string]*serviceInfo),
	}
	chainUnaryServerInterceptors(s)
	return s
}

// 实现grpc.ServiceRegistrar接口
func (s *GRPCServiceRegistrar) RegisterService(sd *grpc.ServiceDesc, ss interface{}) {
	if ss != nil {
		ht := reflect.TypeOf(sd.HandlerType).Elem()
		st := reflect.TypeOf(ss)
		if !st.Implements(ht) {
			logger.Fatalf("grpc: Server.RegisterService found the handler of type %v that does not satisfy %v", st, ht)
		}
	}
	s.register(sd, ss)
}

func (s *GRPCServiceRegistrar) register(sd *grpc.ServiceDesc, ss interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.services[sd.ServiceName]; ok {
		logger.Fatalf("grpc: Server.RegisterService found duplicate service registration for %q", sd.ServiceName)
	}
	info := &serviceInfo{
		serviceImpl: ss,
		methods:     make(map[string]*grpc.MethodDesc),
		mdata:       sd.Metadata,
	}
	for i := range sd.Methods {
		d := &sd.Methods[i]
		info.methods[d.MethodName] = d
	}
	s.services[sd.ServiceName] = info
}

// CallGRPC 根据完整的方法名调用对应的gRPC方法
func (s *GRPCServiceRegistrar) CallGRPC(ctx context.Context, sm string, dec func(any) error) (any, error) {
	if len(sm) == 0 || sm[0] != '/' {
		return nil, status.Error(codes.Unimplemented, fmt.Sprintf("got invalid method %s", sm))
	}

	sm = sm[1:]
	pos := strings.LastIndex(sm, "/")
	if pos == -1 {
		return nil, status.Error(codes.Unimplemented, fmt.Sprintf("got invalid method %s", sm))
	}
	service := sm[:pos]
	method := sm[pos+1:]

	srv, knownService := s.services[service]
	if knownService {
		if md, ok := srv.methods[method]; ok {
			return s.processUnaryRPC(ctx, srv, md, dec)
		}
	}
	return nil, status.Error(codes.Unimplemented, fmt.Sprintf("method %s not implemented", sm))
}

func (s *GRPCServiceRegistrar) processUnaryRPC(ctx context.Context, info *serviceInfo, md *grpc.MethodDesc, df func(any) error) (any, error) {
	reply, appErr := md.Handler(info.serviceImpl, ctx, df, nil)
	if appErr != nil {
		appStatus, ok := status.FromError(appErr)
		if !ok {
			// Convert non-status application error to a status error with code
			// Unknown, but handle context errors specifically.
			appStatus = status.FromContextError(appErr)
			appErr = appStatus.Err()
		}
		return nil, appErr
	}
	return reply, appErr
}

// GetRegisteredServices 获取所有已注册的服务名称
func (d *GRPCServiceRegistrar) GetRegisteredServices() []string {
	services := make([]string, 0, len(d.services))
	for name := range d.services {
		services = append(services, name)
	}
	return services
}

// GetServiceMethods 获取指定服务的所有方法
func (d *GRPCServiceRegistrar) GetServiceMethods(serviceName string) ([]string, error) {
	service, ok := d.services[serviceName]
	if !ok {
		return nil, fmt.Errorf("service not found: %s", serviceName)
	}

	methods := make([]string, 0, len(service.methods))
	for name := range service.methods {
		methods = append(methods, name)
	}
	return methods, nil
}

type serverOptions struct {
	unaryInt       grpc.UnaryServerInterceptor
	chainUnaryInts []grpc.UnaryServerInterceptor
}

// chainUnaryServerInterceptors chains all unary server interceptors into one.
func chainUnaryServerInterceptors(s *GRPCServiceRegistrar) {
	// Prepend opts.unaryInt to the chaining interceptors if it exists, since unaryInt will
	// be executed before any other chained interceptors.
	interceptors := s.opts.chainUnaryInts
	if s.opts.unaryInt != nil {
		interceptors = append([]grpc.UnaryServerInterceptor{s.opts.unaryInt}, s.opts.chainUnaryInts...)
	}

	var chainedInt grpc.UnaryServerInterceptor
	if len(interceptors) == 0 {
		chainedInt = nil
	} else if len(interceptors) == 1 {
		chainedInt = interceptors[0]
	} else {
		chainedInt = chainUnaryInterceptors(interceptors)
	}

	s.opts.unaryInt = chainedInt
}

func chainUnaryInterceptors(interceptors []grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return interceptors[0](ctx, req, info, getChainUnaryHandler(interceptors, 0, info, handler))
	}
}

func getChainUnaryHandler(interceptors []grpc.UnaryServerInterceptor, curr int, info *grpc.UnaryServerInfo, finalHandler grpc.UnaryHandler) grpc.UnaryHandler {
	if curr == len(interceptors)-1 {
		return finalHandler
	}
	return func(ctx context.Context, req any) (any, error) {
		return interceptors[curr+1](ctx, req, info, getChainUnaryHandler(interceptors, curr+1, info, finalHandler))
	}
}
