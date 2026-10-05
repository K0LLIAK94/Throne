package boxmain

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/miekg/dns"
	M "github.com/sagernet/sing/common/metadata"
)

// Private integration input is deliberately external to the repository. Never
// commit subscription credentials or include config content in failure output.
func TestMihomoSubscription(t *testing.T) {
	path := os.Getenv("THRONE_MIHOMO_SUBSCRIPTION")
	if path == "" {
		t.Skip("set THRONE_MIHOMO_SUBSCRIPTION to a private Mihomo JSON subscription")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("cannot read integration subscription")
	}
	var sub struct {
		Proxies []map[string]any `json:"proxies"`
	}
	if json.Unmarshal(content, &sub) != nil || len(sub.Proxies) == 0 {
		t.Fatal("invalid integration subscription")
	}
	for i, proxy := range sub.Proxies {
		t.Run(string(rune('A'+i))+"_"+proxy["type"].(string), func(t *testing.T) {
			config, err := json.Marshal(map[string]any{
				"log":       map[string]any{"disabled": true},
				"outbounds": []any{map[string]any{"type": "mihomo", "tag": "proxy", "server": proxy["server"], "server_port": proxy["port"], "proxy": proxy}},
			})
			if err != nil {
				t.Fatal("cannot serialize integration config")
			}
			if Check(config) != nil {
				t.Fatal("Mihomo profile did not pass CheckConfig")
			}
			instance, cancel, err := Create(config, nil)
			if err != nil {
				t.Fatal("Mihomo profile could not start")
			}
			defer cancel()
			defer instance.Close()
			outbound, ok := instance.Outbound().Outbound("proxy")
			if !ok {
				t.Fatal("missing registered Mihomo outbound")
			}
			client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
					return outbound.DialContext(ctx, network, M.ParseSocksaddr(address))
				},
				DisableKeepAlives: true,
			}}
			target := os.Getenv("THRONE_MIHOMO_TEST_URL")
			if target == "" {
				target = "https://www.cloudflare.com/cdn-cgi/trace"
			}
			response, err := client.Get(target)
			if err != nil {
				t.Fatalf("HTTPS through %s failed: %v", proxy["type"], err)
			}
			defer response.Body.Close()
			if _, err := io.Copy(io.Discard, response.Body); err != nil {
				t.Fatal("proxy response was interrupted")
			}
			if response.StatusCode != 200 && response.StatusCode != 204 {
				t.Fatalf("proxy HTTPS status = %d", response.StatusCode)
			}
			if slices.Contains(outbound.Network(), "udp") {
				ctx, finish := context.WithTimeout(context.Background(), 5*time.Second)
				defer finish()
				destination := M.ParseSocksaddr("1.1.1.1:53")
				packet, err := outbound.ListenPacket(ctx, destination)
				if err != nil {
					t.Fatalf("UDP through %s failed: %v", proxy["type"], err)
				}
				defer packet.Close()
				_ = packet.SetDeadline(time.Now().Add(5 * time.Second))
				query := new(dns.Msg)
				query.SetQuestion("example.com.", dns.TypeA)
				bytes, err := query.Pack()
				if err != nil {
					t.Fatal(err)
				}
				if _, err = packet.WriteTo(bytes, destination.UDPAddr()); err != nil {
					t.Fatal("UDP query failed", err)
				}
				buffer := make([]byte, 4096)
				n, _, err := packet.ReadFrom(buffer)
				if err != nil {
					t.Fatal("UDP response failed", err)
				}
				answer := new(dns.Msg)
				if answer.Unpack(buffer[:n]) != nil || answer.Id != query.Id || len(answer.Answer) == 0 {
					t.Fatal("invalid UDP DNS response")
				}
			} else {
				t.Log("profile does not enable UDP")
			}
		})
	}
}
