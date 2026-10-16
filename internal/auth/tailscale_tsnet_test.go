package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
)

// fakeTsnetNode stands in for *tsnet.Server, whose ListenTLS terminates TLS
// with the node's certificate and whose Listen does not.
type fakeTsnetNode struct {
	cert    tls.Certificate
	gotAddr string
	hang    bool // Up never sees the node running, as with an expired auth key
	client  fakeTsnetClient
}

func (n *fakeTsnetNode) Up(ctx context.Context) (*ipnstate.Status, error) {
	if n.hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	return &ipnstate.Status{CertDomains: []string{"cetacean.example.ts.net"}}, nil
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

func (n *fakeTsnetNode) LocalClient() (tsnetClient, error) {
	return &n.client, nil
}

type fakeTsnetClient struct {
	certErr   error
	gotDomain string
}

func (c *fakeTsnetClient) WhoIs(context.Context, string) (*apitype.WhoIsResponse, error) {
	return nil, errors.New("not used")
}

func (c *fakeTsnetClient) CertPair(_ context.Context, domain string) ([]byte, []byte, error) {
	c.gotDomain = domain
	return nil, nil, c.certErr
}

func testCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	certSource := httptest.NewUnstartedServer(nil)
	certSource.StartTLS()
	certSource.Close()

	return certSource.TLS.Certificates[0]
}

func TestTsnetListenerTerminatesTLS(t *testing.T) {
	node := &fakeTsnetNode{cert: testCertificate(t)}
	_, ln, err := listenTsnet(t.Context(), node, "")
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

func TestTsnetStartupGivesUpOnANodeThatNeverRuns(t *testing.T) {
	node := &fakeTsnetNode{cert: testCertificate(t), hang: true}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	if _, _, err := listenTsnet(ctx, node, ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("listenTsnet = %v, want the deadline", err)
	}
	if node.gotAddr != "" {
		t.Error("listened before the node was up")
	}
}

func TestTsnetStartupFetchesTheCertificate(t *testing.T) {
	node := &fakeTsnetNode{cert: testCertificate(t)}
	_, ln, err := listenTsnet(t.Context(), node, "")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()

	if node.client.gotDomain != "cetacean.example.ts.net" {
		t.Errorf(
			"fetched a certificate for %q, want the node's MagicDNS name",
			node.client.gotDomain,
		)
	}

	node = &fakeTsnetNode{cert: testCertificate(t)}
	node.client.certErr = errors.New("issuance failed")
	if _, _, err := listenTsnet(t.Context(), node, ""); err == nil {
		t.Error("listenTsnet succeeded without a certificate")
	}
}
