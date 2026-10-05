package boxmain

import (
	"context"

	"github.com/sagernet/sing-box/include"

	"ThroneCore/internal/boxbox"
	"ThroneCore/internal/mihomo"
)

func Check(content []byte) error {
	ctx := context.Background()
	outbounds := include.OutboundRegistry()
	mihomo.Register(outbounds)
	ctx = boxbox.Context(ctx, include.InboundRegistry(), outbounds, include.EndpointRegistry(), include.DNSTransportRegistry(), include.ServiceRegistry(), include.CertificateProviderRegistry())
	options, err := parseConfig(ctx, content)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	instance, err := boxbox.New(boxbox.Options{
		Context: ctx,
		Options: *options,
	})
	if err == nil {
		instance.Close()
	}
	cancel()
	return err
}
