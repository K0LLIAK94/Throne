package boxmain

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"ThroneCore/internal/boxbox"
	M "github.com/sagernet/sing/common/metadata"
)

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

func testBox(t *testing.T, config map[string]any) *boxbox.Box {
	t.Helper()
	config["log"] = map[string]any{"disabled": true}
	content, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	box, cancel, err := Create(content, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); box.Close() })
	return box
}

func hostsDNS() map[string]any {
	return map[string]any{
		"servers": []any{map[string]any{"type": "hosts", "tag": "hosts", "predefined": map[string]any{
			"relay.test": "127.0.0.1", "proxied.test": "127.0.0.1", "fail.test": "127.0.0.1", "rejected.test": "127.0.0.1", "udp.test": "127.0.0.1",
		}}},
		"final": "hosts",
	}
}

func socksRelay(t *testing.T) int {
	port := freePort(t)
	testBox(t, map[string]any{
		"inbounds":  []any{map[string]any{"type": "socks", "listen": "127.0.0.1", "listen_port": port}},
		"dns":       hostsDNS(),
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		"route":     map[string]any{"final": "direct", "default_domain_resolver": "hosts"},
	})
	return port
}

func mihomoSocks(tag string, port int) map[string]any {
	return map[string]any{"type": "mihomo", "tag": tag, "server": "relay.test", "server_port": port,
		"proxy": map[string]any{"type": "socks5", "server": "relay.test", "port": port, "udp": true},
	}
}

func TestMihomoRoutingAndNoFallback(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "routed") }))
	defer target.Close()
	targetURL, _ := url.Parse(target.URL)
	_, targetPortText, _ := net.SplitHostPort(targetURL.Host)
	relayPort := socksRelay(t)
	inputPort := freePort(t)
	good := mihomoSocks("good", relayPort)
	bad := mihomoSocks("bad", freePort(t))
	testBox(t, map[string]any{
		"inbounds":  []any{map[string]any{"type": "mixed", "tag": "mixed", "listen": "127.0.0.1", "listen_port": inputPort}},
		"dns":       hostsDNS(),
		"outbounds": []any{good, bad, map[string]any{"type": "direct", "tag": "direct"}},
		"route": map[string]any{"default_domain_resolver": "hosts", "final": "bad", "rules": []any{
			map[string]any{"domain": "proxied.test", "outbound": "good"},
			map[string]any{"domain": "fail.test", "outbound": "bad"},
			map[string]any{"domain": "rejected.test", "action": "reject"},
			map[string]any{"ip_cidr": "127.0.0.0/8", "outbound": "direct"},
		}},
	})
	proxyURL, _ := url.Parse("socks5://127.0.0.1:" + strconv.Itoa(inputPort))
	for _, test := range []struct {
		name, host string
		succeeds   bool
	}{
		{"domain selects Mihomo", "proxied.test", true},
		{"IP CIDR selects direct", "127.0.0.1", true},
		{"failed Mihomo cannot fall back to direct", "fail.test", false},
		{"reject rule remains effective", "rejected.test", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
			response, err := client.Get("http://" + net.JoinHostPort(test.host, targetPortText))
			if test.succeeds {
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err != nil || string(body) != "routed" {
					t.Fatal("wrong routed response", err)
				}
			} else {
				if err == nil {
					response.Body.Close()
					t.Fatal("blocked route unexpectedly reached direct target")
				}
			}
		})
	}
}

func TestMihomoTCPUDPAndDetour(t *testing.T) {
	relay := socksRelay(t)
	transportRelay := socksRelay(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "detoured") }))
	defer target.Close()
	targetURL, _ := url.Parse(target.URL)
	good := mihomoSocks("good", relay)
	good["detour"] = "transport"
	blocked := mihomoSocks("blocked", relay)
	blocked["detour"] = "unreachable"
	box := testBox(t, map[string]any{
		"dns": hostsDNS(),
		"outbounds": []any{good, blocked, map[string]any{"type": "socks", "tag": "transport", "server": "127.0.0.1", "server_port": transportRelay},
			map[string]any{"type": "socks", "tag": "unreachable", "server": "127.0.0.1", "server_port": freePort(t)}},
		"route": map[string]any{"default_domain_resolver": "hosts"},
	})
	outbound, _ := box.Outbound().Outbound("good")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, err := outbound.DialContext(ctx, "tcp", M.ParseSocksaddr(targetURL.Host))
	if err != nil {
		t.Fatal("TCP detour failed", err)
	}
	connection.Close()
	failed, _ := box.Outbound().Outbound("blocked")
	connection, err = failed.DialContext(ctx, "tcp", M.ParseSocksaddr(targetURL.Host))
	if err == nil {
		connection.Close()
		t.Fatal("Mihomo bypassed the configured detour")
	}
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	go func() {
		buffer := make([]byte, 128)
		n, addr, err := udp.ReadFrom(buffer)
		if err == nil {
			udp.WriteTo(buffer[:n], addr)
		}
	}()
	destination := M.SocksaddrFromNet(udp.LocalAddr())
	destination.Fqdn = "udp.test"
	destination.Addr = destination.Addr.Unmap()
	// Exercise domain destinations on both association and per-packet writes.
	domain := M.ParseSocksaddr(net.JoinHostPort("udp.test", strconv.Itoa(int(destination.Port))))
	packet, err := outbound.ListenPacket(ctx, domain)
	if err != nil {
		t.Fatal("UDP detour failed", err)
	}
	defer packet.Close()
	packet.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err = packet.WriteTo([]byte("udp routed"), domain); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 128)
	n, _, err := packet.ReadFrom(buffer)
	if err != nil || string(buffer[:n]) != "udp routed" {
		t.Fatal("UDP reply failed", err)
	}
	if p, err := failed.ListenPacket(ctx, domain); err == nil {
		p.Close()
		t.Fatal("UDP bypassed configured detour")
	}
}
