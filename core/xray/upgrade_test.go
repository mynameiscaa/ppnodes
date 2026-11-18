package xray

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/perfect-panel/ppanel-node/api/panel"
	"github.com/perfect-panel/ppanel-node/common/format"
	"github.com/perfect-panel/ppanel-node/conf"
	ppcore "github.com/perfect-panel/ppanel-node/core"
	"github.com/perfect-panel/ppanel-node/limiter"
	xnet "github.com/xtls/xray-core/common/net"
	xcore "github.com/xtls/xray-core/core"
	xconf "github.com/xtls/xray-core/infra/conf"
)

var testLimiterInit sync.Once

// Exercise the real upstream handlers with PPanel's custom dispatcher. Merely
// compiling does not catch the concrete-dispatcher assertion fixed by the old fork.
func TestXrayProtocolLifecycle(t *testing.T) {
	testLimiterInit.Do(limiter.Init)
	for _, tc := range []struct {
		name, protocol, cipher, network string
		mux, reality, rateLimit         bool
	}{
		{name: "vless", protocol: "vless"},
		{name: "vless-mux", protocol: "vless", mux: true},
		{name: "vless-ws", protocol: "vless", network: "ws"},
		{name: "vless-grpc", protocol: "vless", network: "grpc"},
		{name: "vless-httpupgrade", protocol: "vless", network: "httpupgrade"},
		{name: "vless-xhttp", protocol: "vless", network: "xhttp"},
		{name: "vless-reality-vision", protocol: "vless", reality: true},
		{name: "vless-rate-limit", protocol: "vless", rateLimit: true},
		{name: "vmess", protocol: "vmess"},
		{name: "trojan", protocol: "trojan"},
		{name: "ss-aes128", protocol: "shadowsocks", cipher: "aes-128-gcm"},
		{name: "ss2022-aes128", protocol: "shadowsocks", cipher: "2022-blake3-aes-128-gcm"},
		{name: "ss2022-aes256", protocol: "shadowsocks", cipher: "2022-blake3-aes-256-gcm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newTestXray(t)
			port := freeTestPort(t)
			user := panel.UserInfo{Id: 42, Uuid: "c82b1bd2-0be5-4a06-9676-51d369a22f3d", SpeedLimit: 100}
			if tc.rateLimit {
				user.SpeedLimit = 1
			}
			info := &panel.NodeInfo{Type: tc.protocol, Common: &panel.CommonNode{Protocol: tc.protocol}}
			transport := &panel.TransportConfig{Path: "/upgrade", ServiceName: "upgrade"}
			network := tc.network
			if network == "" {
				network = "tcp"
			}
			stream := map[string]any{"network": network}
			switch network {
			case "ws":
				stream["wsSettings"] = map[string]any{"path": transport.Path}
			case "grpc":
				stream["grpcSettings"] = map[string]any{"serviceName": transport.ServiceName}
			case "httpupgrade":
				stream["httpupgradeSettings"] = map[string]any{"path": transport.Path}
			case "xhttp":
				stream["xhttpSettings"] = map[string]any{"path": transport.Path, "mode": "packet-up"}
			}
			settings := map[string]any{}
			remote := map[string]any{"address": "127.0.0.1", "port": port}
			switch tc.protocol {
			case "vless":
				info.Common.Vless = &panel.VlessNode{Port: port, Network: network, TransportConfig: transport}
				account := map[string]any{"id": user.Uuid, "encryption": "none"}
				if tc.reality {
					info.Common.Vless.Security = "reality"
					info.Common.Vless.Flow = "xtls-rprx-vision"
					account["flow"] = info.Common.Vless.Flow
					info.Common.Vless.SecurityConfig, stream["realitySettings"] = testRealityConfig(t)
					stream["security"] = "reality"
				}
				remote["users"] = []any{account}
				settings["vnext"] = []any{remote}
			case "vmess":
				info.Common.Vmess = &panel.VmessNode{Port: port, Network: "tcp", TransportConfig: transport}
				remote["users"] = []any{map[string]any{"id": user.Uuid, "security": "auto"}}
				settings["vnext"] = []any{remote}
			case "trojan":
				info.Common.Trojan = &panel.TrojanNode{Port: port, Network: "tcp", TransportConfig: transport}
				remote["password"] = user.Uuid
				settings["servers"] = []any{remote}
			case "shadowsocks":
				ss := &panel.ShadowsocksNode{Port: port, Cipher: tc.cipher}
				password := user.Uuid
				if strings.HasPrefix(tc.cipher, "2022-") {
					keyLength := 32
					if strings.Contains(tc.cipher, "aes-128") {
						keyLength = 16
					}
					ss.ServerKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, keyLength))
					password = ss.ServerKey + ":" + base64.StdEncoding.EncodeToString([]byte(user.Uuid[:keyLength]))
				}
				info.Common.Shadowsocks = ss
				remote["method"], remote["password"] = tc.cipher, password
				settings["servers"] = []any{remote}
			}
			options := &conf.Options{ListenIP: "127.0.0.1", XrayOptions: conf.NewXrayOptions()}
			if err := server.AddNode(tc.name, info, options); err != nil {
				t.Fatal(err)
			}
			users := []panel.UserInfo{user}
			limiter.AddLimiter(tc.name, &conf.LimitConfig{}, users, map[int]int{})
			t.Cleanup(func() { limiter.DeleteLimiter(tc.name) })
			if n, err := server.AddUsers(&ppcore.AddUsersParams{Tag: tc.name, Users: users, NodeInfo: info}); err != nil || n != 1 {
				t.Fatalf("AddUsers = %d, %v", n, err)
			}
			manager, err := server.GetUserManager(tc.name)
			if err != nil || manager.GetUser(context.Background(), format.UserTag(tc.name, user.Uuid)) == nil {
				t.Fatalf("dynamic user missing: %v", err)
			}

			outbound := map[string]any{"protocol": tc.protocol, "settings": settings, "streamSettings": stream}
			if tc.mux {
				outbound["mux"] = map[string]any{"enabled": true, "concurrency": 2}
			}
			client := newTestClient(t, outbound)
			target := echoTarget(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			conn, err := xcore.Dial(ctx, client, target)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			timer := time.AfterFunc(10*time.Second, func() { _ = conn.Close() })
			defer timer.Stop()
			payload := bytes.Repeat([]byte("ppanel-xray-upgrade\n"), 256)
			if tc.rateLimit {
				payload = bytes.Repeat([]byte{42}, 256*1024)
			}
			started := time.Now()
			if _, err := conn.Write(payload); err != nil {
				t.Fatal(err)
			}
			got := make([]byte, len(payload))
			if _, err := io.ReadFull(conn, got); err != nil {
				t.Fatalf("proxy echo: %v", err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatal("proxy corrupted the payload")
			}
			if tc.rateLimit && time.Since(started) < time.Second {
				t.Fatal("1 Mbps limit did not throttle the transfer")
			}
			traffic, err := server.GetUserTrafficSlice(tc.name, true)
			if err != nil || len(traffic) != 1 || traffic[0].UID != user.Id || traffic[0].Upload < int64(len(payload)) || traffic[0].Download < int64(len(payload)) {
				t.Fatalf("missing per-user traffic: %+v, %v", traffic, err)
			}
			if traffic, err := server.GetUserTrafficSlice(tc.name, true); err != nil || len(traffic) != 0 {
				t.Fatalf("traffic reset failed: %+v, %v", traffic, err)
			}
			if _, ok := server.dispatcher.LinkManagers.Load(format.UserTag(tc.name, user.Uuid)); !ok {
				t.Fatal("connection was not registered for user removal")
			}
			if err := server.DelUsers(users, tc.name); err != nil {
				t.Fatal(err)
			}
			if manager.GetUser(context.Background(), format.UserTag(tc.name, user.Uuid)) != nil {
				t.Fatal("removed user still exists")
			}
			if _, ok := server.dispatcher.LinkManagers.Load(format.UserTag(tc.name, user.Uuid)); ok {
				t.Fatal("removed user's connections are still registered")
			}
			closed := make(chan error, 1)
			go func() {
				_, err := conn.Read(make([]byte, 1))
				closed <- err
			}()
			select {
			case err := <-closed:
				if err == nil {
					t.Fatal("removed user's connection remained readable")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("removing the user did not close its active connection")
			}
			if err := server.DelNode(tc.name); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestShadowsocksMigrationErrors(t *testing.T) {
	for _, tc := range []struct {
		name, cipher, serverKey, want string
		disableIVCheck                bool
	}{
		{name: "none", cipher: "none", want: "no longer supported"},
		{name: "plain", cipher: "plain", want: "no longer supported"},
		{name: "iv-check", cipher: "aes-128-gcm", disableIVCheck: true, want: "DisableIVCheck"},
		{name: "2022-chacha", cipher: "2022-blake3-chacha20-poly1305", want: "multi-user cipher"},
		{name: "missing-key", cipher: "2022-blake3-aes-128-gcm", want: "server_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := &conf.Options{XrayOptions: conf.NewXrayOptions()}
			options.XrayOptions.DisableIVCheck = tc.disableIVCheck
			info := &panel.NodeInfo{Common: &panel.CommonNode{Shadowsocks: &panel.ShadowsocksNode{Cipher: tc.cipher, ServerKey: tc.serverKey}}}
			err := buildShadowsocks(options, info, &xconf.InboundDetourConfig{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func newTestXray(t *testing.T) *Xray {
	t.Helper()
	t.Setenv("XRAY_LOCATION_ASSET", "")
	t.Setenv("XRAY_DNS_PATH", "")
	config := conf.NewXrayConfig()
	config.LogConfig.Level = "none"
	// New Xray defaults to blocking private destinations. Explicitly permit only
	// the test echo target; keep the application's production defaults intact.
	config.OutboundConfigPath = filepath.Join(t.TempDir(), "outbounds.json")
	if err := os.WriteFile(config.OutboundConfigPath, []byte(`[{"tag":"test-loopback","protocol":"freedom","settings":{"finalRules":[{"action":"allow","ip":["127.0.0.1/32"]}]}}]`), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := New(&conf.CoreConfig{XrayConfig: config})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c.(*Xray)
}

// A local TLS 1.3 + HTTP/2 camouflage target keeps REALITY tests independent
// of public websites, DNS, certificates, and Internet access.
func testRealityConfig(t *testing.T) (*panel.SecurityConfig, map[string]any) {
	t.Helper()
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("camouflage"))
	}))
	target.EnableHTTP2 = true
	target.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	target.StartTLS()
	t.Cleanup(target.Close)
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &panel.SecurityConfig{
		SNI: "example.com", RealityServerAddress: "127.0.0.1",
		RealityServerPort: target.Listener.Addr().(*net.TCPAddr).Port,
		RealityPrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()),
		RealityShortId:    "0123456789abcdef",
	}, map[string]any{
		"serverName": "example.com", "fingerprint": "chrome",
		"publicKey": base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		"shortId":   "0123456789abcdef",
	}
}

func newTestClient(t *testing.T, outbound map[string]any) *xcore.Instance {
	t.Helper()
	data, err := json.Marshal(map[string]any{"log": map[string]any{"loglevel": "none"}, "outbounds": []any{outbound}})
	if err != nil {
		t.Fatal(err)
	}
	var config xconf.Config
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	built, err := config.Build()
	if err != nil {
		t.Fatal(err)
	}
	client, err := xcore.New(built)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func freeTestPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func echoTarget(t *testing.T) xnet.Destination {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	target, err := xnet.ParseDestination(fmt.Sprintf("tcp:%s", l.Addr()))
	if err != nil {
		t.Fatal(err)
	}
	return target
}
