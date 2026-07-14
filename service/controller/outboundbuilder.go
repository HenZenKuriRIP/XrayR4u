package controller

import (
	"encoding/json"
	"fmt"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
)

//OutboundBuilder build freedom outbund config for addoutbound
func OutboundBuilder(config *Config, nodeInfo *api.NodeInfo, tag string) (*core.OutboundHandlerConfig, error) {
	outboundDetourConfig := &conf.OutboundDetourConfig{}
	outboundDetourConfig.Protocol = "freedom"
	outboundDetourConfig.Tag = tag

	// Build Send IP address
	if config.SendIP != "" {
		sendThrough := config.SendIP
		outboundDetourConfig.SendThrough = &sendThrough
	}

	// Freedom Protocol setting
	var domainStrategy string = "Asis"
	if config.EnableDNS {
		if config.DNSType != "" {
			domainStrategy = config.DNSType
		} else {
			domainStrategy = "UseIP"
		}
	}
	proxySetting := &conf.FreedomConfig{
		DomainStrategy: domainStrategy,
	}
	var setting json.RawMessage
	setting, err := json.Marshal(proxySetting)
	if err != nil {
		return nil, fmt.Errorf("Marshal proxy %s config fialed: %s", nodeInfo.NodeType, err)
	}
	outboundDetourConfig.Settings = &setting
	return outboundDetourConfig.Build()
}
