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
	ctx     context.Context
	cancel  context.CancelFunc
	srv     *http.Server
	handler func(string) error
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

// HandleOnStart runs the supplied function with the server URL once the server is listening
func HandleOnStart(onStart func(string) error) ContextServerOption {
	return func(cs *ContextServer) {
		cs.handler = onStart
	}
}

// ListenAndServe will start the server and block, the resulting error may be from start up, start up handlers, or shutdown
func (cs *ContextServer) ListenAndServe() error {
	// Listen separately so we can capture the resolved address
	addr := cs.srv.Addr
	if addr == "" {
		addr = ":http"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	loc := url.URL{Scheme: "http", Host: ln.Addr().String()}

	done := make(chan error, 1)

	// Start the server asynchronously
	go func() {
		if err := cs.srv.Serve(ln); err != http.ErrServerClosed {
			done <- err
		}
	}()

	// Start the shutdown routine
	go func() {
		// Wait for the server context
		<-cs.ctx.Done()

		// Create a context with a 5 second timeout
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Initiate an orderly shutdown, send errors to the channel
		done <- cs.srv.Shutdown(ctx)
	}()

	if cs.handler != nil {
		select {
		case err := <-done:
			// We already hit an error, don't run the handler and requeue the error
			done <- err
		default:
			if err := cs.handler(loc.String()); err != nil {
				// Shutdown the server since the startup handler failed
				cs.cancel()
				done <- err
			}
		}
	}

	return <-done
}