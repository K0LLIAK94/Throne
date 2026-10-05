// Package mihomo embeds Mihomo's protocol adapters as sing-box outbounds.
// Throne's sing-box instance remains the only owner of routing, DNS and TUN.
package mihomo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"

	meta "github.com/metacubex/mihomo/adapter"
	MC "github.com/metacubex/mihomo/constant"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

const Type = "mihomo"

type Options struct {
	option.DialerOptions
	option.ServerOptions
	Proxy map[string]any `json:"proxy"`
}

func Register(registry *outbound.Registry) {
	outbound.Register[Options](registry, Type, New)
}

var _ adapter.Outbound = (*Outbound)(nil)

type Outbound struct {
	outbound.Adapter
	ctx     context.Context
	logger  log.ContextLogger
	dns     adapter.DNSRouter
	query   adapter.DNSQueryOptions
	mapping map[string]any
	dialer  N.Dialer
	mu      sync.Mutex
	proxy   MC.Proxy
	closed  bool
}

func New(ctx context.Context, _ adapter.Router, logger log.ContextLogger, tag string, options Options) (adapter.Outbound, error) {
	// Copy input: neither Mihomo's decoder nor runtime hostname resolution may
	// modify the persisted profile or another concurrent test's configuration.
	content, err := json.Marshal(options.Proxy)
	if err != nil {
		return nil, err
	}
	var mapping map[string]any
	if err = json.Unmarshal(content, &mapping); err != nil {
		return nil, err
	}
	if mapping == nil {
		return nil, errors.New("missing Mihomo proxy")
	}
	if mapping["dialer-proxy"] != nil && mapping["dialer-proxy"] != "" {
		return nil, errors.New("Mihomo dialer-proxy must be expressed as a Throne chain")
	}
	// The enclosing tag owns routing; names from a subscription are display data.
	mapping["name"] = tag
	upstream, err := dialer.New(ctx, options.DialerOptions, true)
	if err != nil {
		return nil, err
	}
	query, err := dialer.NewDNSQueryOptions(ctx, options.DomainResolver, false)
	if err != nil {
		return nil, err
	}
	o := &Outbound{
		ctx: ctx, logger: logger, dns: service.FromContext[adapter.DNSRouter](ctx),
		mapping: mapping, dialer: upstream, query: query,
	}
	// Parse once without dialing so CheckConfig reports unsupported protocols and
	// invalid credentials before a profile can be selected by a routing rule.
	proxy, err := meta.ParseProxy(mapping, meta.WithDialerForAPI(egress{upstream}))
	if err != nil {
		return nil, fmt.Errorf("Mihomo proxy: %w", err)
	}
	networks := []string{N.NetworkTCP}
	if proxy.SupportUDP() {
		networks = append(networks, N.NetworkUDP)
	}
	o.Adapter = outbound.NewAdapterWithDialerOptions(Type, tag, networks, options.DialerOptions)
	if host, _ := mapping["server"].(string); isDomain(host) {
		// QUIC and Mieru also resolve outside DialerForAPI. Supply an IP using
		// Throne's bootstrap resolver, preserving the original TLS server name.
		_ = proxy.Close()
	} else {
		o.proxy = proxy
	}
	return o, nil
}

func isDomain(host string) bool {
	_, err := netip.ParseAddr(host)
	return host != "" && err != nil
}

func (o *Outbound) getProxy(ctx context.Context) (MC.Proxy, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil, net.ErrClosed
	}
	if o.proxy != nil {
		return o.proxy, nil
	}
	host, _ := o.mapping["server"].(string)
	ips, err := o.dns.Lookup(ctx, host, o.query)
	if err != nil {
		return nil, fmt.Errorf("resolve Mihomo server: %w", err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no address for Mihomo server %s", host)
	}
	mapping := make(map[string]any, len(o.mapping))
	for k, v := range o.mapping {
		mapping[k] = v
	}
	mapping["server"] = ips[0].String()
	for _, key := range []string{"sni", "servername"} {
		if value, _ := mapping[key].(string); value == "" {
			mapping[key] = host
		}
	}
	o.proxy, err = meta.ParseProxy(mapping, meta.WithDialerForAPI(egress{o.dialer}))
	return o.proxy, err
}

func (o *Outbound) connectionContext(ctx context.Context, destination M.Socksaddr) context.Context {
	ctx, metadata := adapter.ExtendContext(ctx)
	metadata.Outbound = o.Tag()
	metadata.Destination = destination
	return ctx
}

func metadata(destination M.Socksaddr, network MC.NetWork) *MC.Metadata {
	return &MC.Metadata{
		Host: destination.Fqdn, DstIP: destination.Addr, DstPort: destination.Port,
		NetWork: network, Type: MC.INNER,
	}
}

func (o *Outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	ctx = o.connectionContext(ctx, destination)
	if N.NetworkName(network) == N.NetworkUDP {
		pc, err := o.ListenPacket(ctx, destination)
		if err != nil {
			return nil, err
		}
		return bufio.NewBindPacketConn(pc, destination), nil
	}
	if N.NetworkName(network) != N.NetworkTCP {
		return nil, N.ErrUnknownNetwork
	}
	proxy, err := o.getProxy(ctx)
	if err != nil {
		return nil, err
	}
	o.logger.InfoContext(ctx, "Mihomo outbound connection to ", destination)
	return proxy.DialContext(ctx, metadata(destination, MC.TCP))
}

func (o *Outbound) resolveDestination(ctx context.Context, destination M.Socksaddr) (M.Socksaddr, error) {
	if !destination.IsDomain() {
		return destination, nil
	}
	ips, err := o.dns.Lookup(ctx, destination.Fqdn, adapter.DNSQueryOptions{})
	if err != nil {
		return M.Socksaddr{}, err
	}
	if len(ips) == 0 {
		return M.Socksaddr{}, fmt.Errorf("no address for %s", destination.Fqdn)
	}
	destination.Addr = ips[0]
	return destination, nil
}

func (o *Outbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	ctx = o.connectionContext(ctx, destination)
	proxy, err := o.getProxy(ctx)
	if err != nil {
		return nil, err
	}
	if !proxy.SupportUDP() {
		return nil, fmt.Errorf("Mihomo %s does not support UDP", proxy.Type())
	}
	destination, err = o.resolveDestination(ctx, destination)
	if err != nil {
		return nil, err
	}
	o.logger.InfoContext(ctx, "Mihomo outbound packet connection to ", destination)
	pc, err := proxy.ListenPacketContext(ctx, metadata(destination, MC.UDP))
	if err != nil {
		return nil, err
	}
	return &packetConn{PacketConn: pc, ctx: ctx, owner: o}, nil
}

func (o *Outbound) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	if o.proxy != nil {
		return o.proxy.Close()
	}
	return nil
}

// Every upstream socket, including QUIC packets, uses sing-box's dialer. That
// preserves detours, interface binding, socket marks and TUN loop protection.
type egress struct{ N.Dialer }

func (e egress) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return e.Dialer.DialContext(ctx, network, M.ParseSocksaddr(address))
}

func (e egress) ListenPacket(ctx context.Context, _, _ string, destination netip.AddrPort) (net.PacketConn, error) {
	return e.Dialer.ListenPacket(ctx, M.Socksaddr{Addr: destination.Addr(), Port: destination.Port()})
}

type packetConn struct {
	net.PacketConn
	ctx   context.Context
	owner *Outbound
}

func (p *packetConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	destination, err := p.owner.resolveDestination(p.ctx, M.SocksaddrFromNet(addr))
	if err != nil {
		return 0, err
	}
	return p.PacketConn.WriteTo(b, destination.UDPAddr())
}
