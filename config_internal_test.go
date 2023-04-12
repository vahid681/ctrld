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
	if uc.BootstrapIP == "" {
		t.Log(availableNameservers())
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
			},
			&UpstreamConfig{
				Name:        "dot",
				Type:        "dot",
				Endpoint:    "freedns.controld.com:853",
				BootstrapIP: "",
				Domain:      "freedns.controld.com",
				Timeout:     0,
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
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.uc.Init()
			assert.Equal(t, tc.expected, tc.uc)
		})
	}
}
