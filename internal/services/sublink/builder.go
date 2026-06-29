// Package sublink builds share URIs and subscription payloads for clients.
package sublink

import (
	"net"
	"net/url"
	"strconv"

	"sing-box-web-panel/internal/domain"
)

// BuildLink returns a share URI for the client on its inbound, addressed at
// host. It returns "" for unsupported protocols.
func BuildLink(ib *domain.Inbound, c *domain.Client, host string) string {
	port := strconv.Itoa(ib.Port)
	switch ib.Protocol {
	case domain.ProtocolVLESS:
		return buildVLESS(ib, c, host, port)
	case domain.ProtocolHysteria2:
		return buildHysteria2(ib, c, host, port)
	case domain.ProtocolNaive:
		return buildNaive(ib, c, host, port)
	default:
		return ""
	}
}

func buildVLESS(ib *domain.Inbound, c *domain.Client, host, port string) string {
	q := url.Values{}
	q.Set("encryption", "none")
	q.Set("type", string(ib.Transmission))

	switch ib.TLS {
	case domain.TLSModeReality:
		q.Set("security", "reality")
		q.Set("sni", ib.SNI)
		q.Set("pbk", ib.Settings.RealityPublicKey)
		if ib.Settings.RealityShortID != "" {
			q.Set("sid", ib.Settings.RealityShortID)
		}
		q.Set("fp", "chrome")
		if ib.Settings.Flow != "" {
			q.Set("flow", ib.Settings.Flow)
		}
	case domain.TLSModeTLS:
		q.Set("security", "tls")
		if ib.SNI != "" {
			q.Set("sni", ib.SNI)
		}
		if ib.EffectiveAllowInsecure() {
			q.Set("allowInsecure", "1")
		}
		q.Set("fp", "chrome")
	default:
		q.Set("security", "none")
	}

	switch ib.Transmission {
	case domain.TransmissionWS:
		if ib.Settings.WSPath != "" {
			q.Set("path", ib.Settings.WSPath)
		}
		if ib.SNI != "" {
			q.Set("host", ib.SNI)
		}
	case domain.TransmissionGRPC:
		if ib.Settings.GRPCServiceName != "" {
			q.Set("serviceName", ib.Settings.GRPCServiceName)
		}
		q.Set("mode", "gun")
	case domain.TransmissionHTTPUpgrade:
		if ib.Settings.HTTPUpgradePath != "" {
			q.Set("path", ib.Settings.HTTPUpgradePath)
		}
		if ib.Settings.HTTPUpgradeHost != "" {
			q.Set("host", ib.Settings.HTTPUpgradeHost)
		}
	}

	u := url.URL{
		Scheme:   "vless",
		User:     url.User(c.UUID),
		Host:     net.JoinHostPort(host, port),
		RawQuery: q.Encode(),
		Fragment: c.Name,
	}
	return u.String()
}

func buildHysteria2(ib *domain.Inbound, c *domain.Client, host, port string) string {
	q := url.Values{}
	if ib.SNI != "" {
		q.Set("sni", ib.SNI)
	}
	if ib.Settings.Hy2ObfsPassword != "" {
		q.Set("obfs", "salamander")
		q.Set("obfs-password", ib.Settings.Hy2ObfsPassword)
	}
	if ib.EffectiveAllowInsecure() {
		q.Set("insecure", "1")
	} else {
		q.Set("insecure", "0")
	}

	u := url.URL{
		Scheme:   "hysteria2",
		User:     url.User(c.Password),
		Host:     net.JoinHostPort(host, port),
		RawQuery: q.Encode(),
		Fragment: c.Name,
	}
	return u.String()
}

func buildNaive(ib *domain.Inbound, c *domain.Client, host, port string) string {
	q := url.Values{}
	if ib.EffectiveAllowInsecure() {
		q.Set("allowInsecure", "1")
	}

	u := url.URL{
		Scheme:   "naive+https",
		User:     url.UserPassword(c.Name, c.Password),
		Host:     net.JoinHostPort(host, port),
		RawQuery: q.Encode(),
		Fragment: c.Name,
	}
	return u.String()
}
