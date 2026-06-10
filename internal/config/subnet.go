package config

import (
	"log/slog"
	"net"
	"strings"
)

// SubnetManager mirrors Config.SubnetManager: it decides whether a request may
// have anonymous access based on the remote address, the configured proxy-server
// subnets, and an X-Forwarded-For header from a trusted proxy.
type SubnetManager struct {
	ProxyServers     []*net.IPNet
	AnonymousSubnets []*net.IPNet
}

// NewSubnetManager parses CIDR strings; invalid entries are logged and dropped
// (mirrors LocusMain.anonymousSubnetManager).
func NewSubnetManager(proxy, anonymous []string) SubnetManager {
	return SubnetManager{
		ProxyServers:     parseCIDRs(proxy),
		AnonymousSubnets: parseCIDRs(anonymous),
	}
}

func parseCIDRs(in []string) []*net.IPNet {
	var out []*net.IPNet
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		_, ipnet, err := net.ParseCIDR(s)
		if err != nil {
			slog.Error("invalid subnet in config", "subnet", s, "err", err)
			continue
		}
		out = append(out, ipnet)
	}
	return out
}

func contains(nets []*net.IPNet, ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// IsInSubnet mirrors SubnetManager.isInSubnet.
//
// remoteAddr is the connecting peer's IP. xff is the X-Forwarded-For header
// value (empty if absent). For an IPv4 peer that is a configured proxy server,
// the forwarded address is used; for an IPv6 loopback peer the forwarded
// address (or 127.0.0.1) is used; otherwise the peer address itself is checked.
func (m SubnetManager) IsInSubnet(remoteAddr net.IP, xff string) bool {
	inAnon := func(addrStr string) bool {
		ip := net.ParseIP(strings.TrimSpace(addrStr))
		return contains(m.AnonymousSubnets, ip)
	}

	isProxyServer := contains(m.ProxyServers, remoteAddr)

	switch {
	case remoteAddr.To4() != nil:
		resolved := remoteAddr.String()
		if isProxyServer && xff != "" {
			resolved = xff
		}
		return inAnon(resolved)
	case remoteAddr.Equal(net.IPv6loopback):
		if xff != "" {
			return inAnon(xff)
		}
		return inAnon("127.0.0.1")
	default:
		return false
	}
}
