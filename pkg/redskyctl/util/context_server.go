/*
Copyright 2020 GramLabs, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// ContextServer is a server bound to a context lifecycle
type ContextServer struct {
	ctx    context.Context
	cancel context.CancelFunc
	srv    *http.Server

	startUp func(string) error
}

type ContextServerOption func(*ContextServer)

func NewContextServer(ctx context.Context, handler http.Handler, options ...ContextServerOption) *ContextServer {
	cs := &ContextServer{}

	// Create a cancellable context we can use to shut down the server
	cs.ctx, cs.cancel = context.WithCancel(ctx)

	// Create a default HTTP server
	cs.srv = &http.Server{Handler: handler}

	// Apply the options
	for _, o := range options {
		o(cs)
	}

	return cs
}

// WithServerOptions exposes the
func WithServerOptions(serverOptions func(*http.Server)) ContextServerOption {
	return func(cs *ContextServer) {
		serverOptions(cs.srv)
	}
}

// ShutdownOnInterrupt shuts the server down in response to a SIGINT or SIGTERM
func ShutdownOnInterrupt(onInterrupt func()) ContextServerOption {
	return func(cs *ContextServer) {
		go func() {
			// Wait for an interrupt signal
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
			<-quit

			// Call the supplied interrupt handler and shutdown the server
			if onInterrupt != nil {
				onInterrupt()
			}
			cs.cancel()
		}()
	}
}

// HandleStart runs the supplied function with the server URL once the server is listening
func HandleStart(startUp func(string) error) ContextServerOption {
	return func(cs *ContextServer) {
		cs.startUp = startUp
	}
}

// ListenAndServe will start the server and block, the resulting error may be from start up, start up handlers, or shutdown
func (cs *ContextServer) ListenAndServe() error {
	// Listen separately from serve so we can capture the resolved address
	l, loc, err := listen(cs.srv.Addr)
	if err != nil {
		return err
	}

	// Start the server and the shutdown routine asynchronously
	done := make(chan error, 1)
	go cs.asyncServe(l, done)
	go cs.asyncShutdown(done)

	// Run the start up handler
	cs.handleStartUp(loc, done)

	return <-done
}

func listen(addr string) (net.Listener, *url.URL, error) {
	if addr == "" {
		addr = ":http"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	// Dummy reverse lookup for loopback/unspecified
	loc := url.URL{Scheme: "http", Host: ln.Addr().String(), Path: "/"}
	if ip := net.ParseIP(loc.Hostname()); ip != nil && (ip.IsLoopback() || ip.IsUnspecified()) {
		loc.Host = net.JoinHostPort("localhost", loc.Port())
	}

	return ln, &loc, nil
}

func (cs *ContextServer) asyncServe(l net.Listener, done chan error) {
	if err := cs.srv.Serve(l); err != http.ErrServerClosed {
		done <- err
	}
}

func (cs *ContextServer) asyncShutdown(done chan error) {
	// Wait for the server context
	<-cs.ctx.Done()

	// Create a context with a 5 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Initiate an orderly shutdown, send errors to the channel
	done <- cs.srv.Shutdown(ctx)
}

func (cs *ContextServer) handleStartUp(loc *url.URL, done chan error) {
	if cs.startUp != nil {
		select {
		case err := <-done:
			// We already hit an error, don't run the handler and requeue
			done <- err
		default:
			if err := cs.startUp(loc.String()); err != nil {
				// Shutdown the server since the startup handler failed
				cs.cancel()
				done <- err
			}
		}
	}
}
