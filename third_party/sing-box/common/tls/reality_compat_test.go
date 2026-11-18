//go:build with_utls

package tls

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"

	"github.com/sagernet/sing-box/option"
)

func TestRealityPrivateKeyCompatibility(t *testing.T) {
	key := bytes.Repeat([]byte{1}, 32)
	options := option.InboundTLSOptions{
		ServerName: "example.com",
		Reality: &option.InboundRealityOptions{
			Enabled: true, PrivateKey: base64.RawURLEncoding.EncodeToString(key),
			Handshake: option.InboundRealityHandshakeOptions{
				ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: 443},
			},
		},
	}
	server, err := NewRealityServer(context.Background(), nil, options)
	if err != nil {
		t.Fatal(err)
	}
	if server.config.PrivateKey == nil || !bytes.Equal(server.config.PrivateKey.Bytes(), key) {
		t.Fatal("existing Base64 private key was not preserved")
	}
	for _, invalid := range []string{"not-a-valid-key", base64.RawURLEncoding.EncodeToString(key[:16])} {
		options.Reality.PrivateKey = invalid
		if _, err := NewRealityServer(context.Background(), nil, options); err == nil {
			t.Fatal("invalid REALITY private key was accepted")
		}
	}
}
