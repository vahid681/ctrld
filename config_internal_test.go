package ctrld

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpstreamConfig_SetupBootstrapIP(t *testing.T) {
	uc := &UpstreamConfig{
		Name:     "test",
		Type:     ResolverTypeDOH,
		Endpoint: "https://freedns.controld.com/p2",
		Timeout:  5000,
	}
	uc.Init()
	uc.setupBootstrapIP(false)
	if len(uc.bootstrapIPs) == 0 {
		t.Log(nameservers())
		t.Fatal("could not bootstrap ip without bootstrap DNS")
	}
	t.Log(uc)
}

func TestUpstreamConfig_Init(t *testing.T) {
	u1, _ := url.Parse("https://example.com")
	u2, _ := url.Parse("https://example.com?k=v")
	tests := []struct {
		name     string
		uc       *UpstreamConfig
		expected *UpstreamConfig
	}{
		{
			"doh+doh3",
			&UpstreamConfig{
				Name:        "doh",
				Type:        "doh",
				Endpoint:    "https://example.com",
				BootstrapIP: "",
				Domain:      "",
				Timeout:     0,
			},
			&UpstreamConfig{
				Name:        "doh",
				Type:        "doh",
				Endpoint:    "https://example.com",
				BootstrapIP: "",
				Domain:      "example.com",
				Timeout:     0,
				IPStack:     IpStackBoth,
				u:           u1,
			},
		},
		{
			"doh+doh3 with query param",
			&UpstreamConfig{
				Name:        "doh",
				Type:        "doh",
				Endpoint:    "https://example.com?k=v",
				BootstrapIP: "",
				Domain:      "",
				Timeout:     0,
			},
			&UpstreamConfig{
				Name:        "doh",
				Type:        "doh",
				Endpoint:    "https://example.com?k=v",
				BootstrapIP: "",
				Domain:      "example.com",
				Timeout:     0,
				IPStack:     IpStackBoth,
				u:           u2,
			},
		},
		{
			"dot+doq",
			&UpstreamConfig{
				Name:        "dot",
				Type:        "dot",
				Endpoint:    "freedns.controld.com:8853",
				BootstrapIP: "",
				Domain:      "",
				Timeout:     0,
			},
			&UpstreamConfig{
				Name:        "dot",
				Type:        "dot",
				Endpoint:    "freedns.controld.com:8853",
				BootstrapIP: "",
				Domain:      "freedns.controld.com",
				Timeout:     0,
				IPStack:     IpStackSplit,
			},
		},
		{
			"dot+doq without port",
			&UpstreamConfig{
				Name:        "dot",
				Type:        "dot",
				Endpoint:    "freedns.controld.com",
				BootstrapIP: "",
				Domain:      "",
				Timeout:     0,
				IPStack:     IpStackSplit,
			},
			&UpstreamConfig{
				Name:        "dot",
				Type:        "dot",
				Endpoint:    "freedns.controld.com:853",
				BootstrapIP: "",
				Domain:      "freedns.controld.com",
				Timeout:     0,
				IPStack:     IpStackSplit,
			},
		},
		{
			"legacy",
			&UpstreamConfig{
				Name:        "legacy",
				Type:        "legacy",
				Endpoint:    "1.2.3.4:53",
				BootstrapIP: "",
				Domain:      "",
				Timeout:     0,
			},
			&UpstreamConfig{
				Name:        "legacy",
				Type:        "legacy",
				Endpoint:    "1.2.3.4:53",
				BootstrapIP: "1.2.3.4",
				Domain:      "1.2.3.4",
				Timeout:     0,
				IPStack:     IpStackBoth,
			},
		},
		{
			"legacy without port",
			&UpstreamConfig{
				Name:        "legacy",
				Type:        "legacy",
				Endpoint:    "1.2.3.4",
				BootstrapIP: "",
				Domain:      "",
				Timeout:     0,
			},
			&UpstreamConfig{
				Name:        "legacy",
				Type:        "legacy",
				Endpoint:    "1.2.3.4:53",
				BootstrapIP: "1.2.3.4",
				Domain:      "1.2.3.4",
				Timeout:     0,
				IPStack:     IpStackBoth,
			},
		},
		{
			"doh+doh3 with send client info set",
			&UpstreamConfig{
				Name:           "doh",
				Type:           "doh",
				Endpoint:       "https://example.com?k=v",
				BootstrapIP:    "",
				Domain:         "",
				Timeout:        0,
				SendClientInfo: ptrBool(false),
				IPStack:        IpStackBoth,
			},
			&UpstreamConfig{
				Name:           "doh",
				Type:           "doh",
				Endpoint:       "https://example.com?k=v",
				BootstrapIP:    "",
				Domain:         "example.com",
				Timeout:        0,
				SendClientInfo: ptrBool(false),
				IPStack:        IpStackBoth,
				u:              u2,
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.uc.Init()
			tc.uc.uid = "" // we don't care about the uid.
			assert.Equal(t, tc.expected, tc.uc)
		})
	}
}

func TestUpstreamConfig_VerifyDomain(t *testing.T) {
	tests := []struct {
		name         string
		uc           *UpstreamConfig
		verifyDomain string
	}{
		{
			controlDComDomain,
			&UpstreamConfig{Endpoint: "https://freedns.controld.com/p2"},
			controldVerifiedDomain[controlDComDomain],
		},
		{
			controlDDevDomain,
			&UpstreamConfig{Endpoint: "https://freedns.controld.dev/p2"},
			controldVerifiedDomain[controlDDevDomain],
		},
		{
			"non-ControlD upstream",
			&UpstreamConfig{Endpoint: "https://dns.google/dns-query"},
			"",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.uc.VerifyDomain(); got != tc.verifyDomain {
				t.Errorf("unexpected verify domain, want: %q, got: %q", tc.verifyDomain, got)
			}
		})
	}
}

func TestUpstreamConfig_UpstreamSendClientInfo(t *testing.T) {
	tests := []struct {
		name           string
		uc             *UpstreamConfig
		sendClientInfo bool
	}{
		{
			"default with controld upstream DoH",
			&UpstreamConfig{Endpoint: "https://freedns.controld.com/p2", Type: ResolverTypeDOH},
			true,
		},
		{
			"default with controld upstream DoH3",
			&UpstreamConfig{Endpoint: "https://freedns.controld.com/p2", Type: ResolverTypeDOH3},
			true,
		},
		{
			"default with non-ControlD upstream",
			&UpstreamConfig{Endpoint: "https://dns.google/dns-query", Type: ResolverTypeDOH},
			false,
		},
		{
			"set false with controld upstream",
			&UpstreamConfig{Endpoint: "https://freedns.controld.com/p2", Type: ResolverTypeDOH, SendClientInfo: ptrBool(false)},
			false,
		},
		{
			"set true with controld upstream",
			&UpstreamConfig{Endpoint: "https://freedns.controld.com/p2", SendClientInfo: ptrBool(true)},
			true,
		},
		{
			"set false with non-ControlD upstream",
			&UpstreamConfig{Endpoint: "https://dns.google/dns-query", SendClientInfo: ptrBool(false)},
			false,
		},
		{
			"set true with non-ControlD upstream",
			&UpstreamConfig{Endpoint: "https://dns.google/dns-query", Type: ResolverTypeDOH, SendClientInfo: ptrBool(true)},
			true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.uc.UpstreamSendClientInfo(); got != tc.sendClientInfo {
				t.Errorf("unexpected result, want: %v, got: %v", tc.sendClientInfo, got)
			}
		})
	}
}

func ptrBool(b bool) *bool {
	return &b
}
