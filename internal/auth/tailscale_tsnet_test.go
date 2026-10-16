package auth

import (
	"crypto/tls"
	"net"
	"net/http/httptest"
	"testing"

	"tailscale.com/client/local"
)

// fakeTsnetNode stands in for *tsnet.Server, whose ListenTLS terminates TLS
// with the node's certificate and whose Listen does not.
type fakeTsnetNode struct {
	cert    tls.Certificate
	gotAddr string
}

func (n *fakeTsnetNode) Listen(_, addr string) (net.Listener, error) {
	n.gotAddr = addr
	return net.Listen("tcp", "127.0.0.1:0")
}

func (n *fakeTsnetNode) ListenTLS(network, addr string) (net.Listener, error) {
	ln, err := n.Listen(network, addr)
	if err != nil {
		return nil, err
	}

	return tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{n.cert}}), nil
}

func (n *fakeTsnetNode) LocalClient() (*local.Client, error) {
	return &local.Client{}, nil
}

func TestTsnetListenerTerminatesTLS(t *testing.T) {
	certSource := httptest.NewUnstartedServer(nil)
	certSource.StartTLS()
	certSource.Close()

	node := &fakeTsnetNode{cert: certSource.TLS.Certificates[0]}
	_, ln, err := listenTsnet(node, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	if node.gotAddr != ":443" {
		t.Errorf("listened on %q, want :443", node.gotAddr)
	}

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		if tc, ok := conn.(*tls.Conn); ok {
			_ = tc.Handshake()
		}
	}()

	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // test certificate
	})
	if err != nil {
		t.Fatalf("TLS handshake with the tsnet listener: %v", err)
	}
	conn.Close()
}
