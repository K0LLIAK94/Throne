# Mihomo protocol support

This fork embeds Mihomo v1.19.32 protocol adapters inside ThroneCore. sing-box
continues to own the inbounds, TUN, routing rules, DNS, selectors and traffic
accounting. There is no second TUN or Mihomo routing/controller configuration.

Clash/Mihomo JSON subscriptions (`proxies`) and YAML subscriptions are imported
as ordinary custom outbounds. The complete proxy object is retained, including
options such as XHTTP, VLESS encryption, Hysteria2 Gecko, ShadowQUIC and Sudoku.
The profile table shows the protocol's name and server address. Existing native
profiles continue to use their existing adapters.

The default subscription user agent is exactly `mihomo/1.19.32` so providers can
return their full Mihomo configuration. Appending `Throne` can make providers
select their limited native link format instead. Explicit global or group user agents
still take precedence. Only individual proxy definitions are imported; remote
subscription rules and proxy groups do not replace Throne routing rules.

Every upstream TCP and UDP socket uses a sing-box dialer. Throne chains,
interface binding and socket marks therefore apply to Mihomo outbounds too.
Upstream server hostnames and UDP destination hostnames resolve through
sing-box DNS rather than Mihomo's global resolver. A failed proxy does not
fall back to direct. `dialer-proxy` references in raw Mihomo objects must instead
be represented as a Throne chain.

Supported protocols are those implemented by the pinned Mihomo version, plus
Throne's existing native and Xray support. This does not promise every protocol
that could exist, and UDP is only enabled when the imported profile enables it.
Raw Mihomo profiles use the existing custom outbound JSON editor.

When an imported Mihomo profile does not enable UDP, the generated remote UDP
DNS server is switched to TCP at the same resolver address and port. Incoming
UDP DNS queries (including TUN DNS hijacking) still work through that TCP path.
DNS rules and the proxy detour remain owned by sing-box; no direct fallback is
introduced. This does not enable general UDP traffic on a TCP-only profile.
A manually supplied raw DNS object is used unchanged.

## Verification

Run the existing Windows core build tags and the routing tests:

```sh
cd core
go test -ldflags=-checklinkname=0 -tags 'with_clash_api,with_quic,with_wireguard,with_utls,with_dhcp,with_tailscale,with_openvpn,with_openconnect,badlinkname,tfogo_checklinkname0,with_purego,with_naive_outbound' ./internal/...
```

Configure the GUI with `-DTHRONE_BUILD_TESTS=ON`, build it, and run `ctest`.
`mihomo-import-test /path/to/private-subscription.json` additionally checks that
every proxy object in a real JSON subscription survives wrapping unchanged.

On Windows, run `python tests/mihomo_subscription_windows.py --gui build/Throne.exe
--core-dir deployment/windows-amd64` to exercise real GUI HTTP subscription
refreshes against a local provider. It checks the default User-Agent and explicit
global/group overrides, and compares every imported proxy field. All databases
and processes are isolated from the user's configuration.

For opt-in live HTTPS and UDP checks, set `THRONE_MIHOMO_SUBSCRIPTION` to a
private JSON subscription file outside the checkout and run
`go test -v ./internal/boxmain -run TestMihomoSubscription`. The test uses each
Mihomo outbound directly, with no direct fallback. Never commit that file or
its credentials. `THRONE_MIHOMO_TEST_URL` can override the HTTPS test endpoint.

Run `python tests/mihomo_dns_windows.py --gui build/Throne.exe --core-dir deployment/windows-amd64`
to check real GUI DNS hijacking against local DNS and SOCKS servers. It verifies
TCP upstream DNS through a UDP-disabled Mihomo profile
and preserves UDP DNS through a UDP-enabled profile at the same resolver port.
The test never enables TUN or changes the system proxy.
