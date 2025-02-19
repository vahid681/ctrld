package ctrld

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"

	"github.com/cuonglm/osinfo"

	"github.com/miekg/dns"
)

const (
	dohMacHeader         = "x-cd-mac"
	dohIPHeader          = "x-cd-ip"
	dohHostHeader        = "x-cd-host"
	dohOsHeader          = "x-cd-os"
	headerApplicationDNS = "application/dns-message"
)

// EncodeOsNameMap provides mapping from OS name to a shorter string, used for encoding x-cd-os value.
var EncodeOsNameMap = map[string]string{
	"windows": "1",
	"darwin":  "2",
	"linux":   "3",
	"freebsd": "4",
}

// DecodeOsNameMap provides mapping from encoded OS name to real value, used for decoding x-cd-os value.
var DecodeOsNameMap = map[string]string{}

// EncodeArchNameMap provides mapping from OS arch to a shorter string, used for encoding x-cd-os value.
var EncodeArchNameMap = map[string]string{
	"amd64":  "1",
	"arm64":  "2",
	"arm":    "3",
	"386":    "4",
	"mips":   "5",
	"mipsle": "6",
	"mips64": "7",
}

// DecodeArchNameMap provides mapping from encoded OS arch to real value, used for decoding x-cd-os value.
var DecodeArchNameMap = map[string]string{}

func init() {
	for k, v := range EncodeOsNameMap {
		DecodeOsNameMap[v] = k
	}
	for k, v := range EncodeArchNameMap {
		DecodeArchNameMap[v] = k
	}
}

// TODO: use sync.OnceValue when upgrading to go1.21
var xCdOsValueOnce sync.Once
var xCdOsValue string

func dohOsHeaderValue() string {
	xCdOsValueOnce.Do(func() {
		oi := osinfo.New()
		xCdOsValue = strings.Join([]string{EncodeOsNameMap[runtime.GOOS], EncodeArchNameMap[runtime.GOARCH], oi.Dist}, "-")
	})
	return xCdOsValue
}

func newDohResolver(uc *UpstreamConfig) *dohResolver {
	r := &dohResolver{
		endpoint:          uc.u,
		isDoH3:            uc.Type == ResolverTypeDOH3,
		http3RoundTripper: uc.http3RoundTripper,
		sendClientInfo:    uc.UpstreamSendClientInfo(),
		uc:                uc,
	}
	return r
}

type dohResolver struct {
	uc                *UpstreamConfig
	endpoint          *url.URL
	isDoH3            bool
	http3RoundTripper http.RoundTripper
	sendClientInfo    bool
}

func (r *dohResolver) Resolve(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	data, err := msg.Pack()
	if err != nil {
		return nil, err
	}

	enc := base64.RawURLEncoding.EncodeToString(data)
	query := r.endpoint.Query()
	query.Add("dns", enc)

	endpoint := *r.endpoint
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}
	addHeader(ctx, req, r.sendClientInfo)
	dnsTyp := uint16(0)
	if len(msg.Question) > 0 {
		dnsTyp = msg.Question[0].Qtype
	}
	c := http.Client{Transport: r.uc.dohTransport(dnsTyp)}
	if r.isDoH3 {
		transport := r.uc.doh3Transport(dnsTyp)
		if transport == nil {
			return nil, errors.New("DoH3 is not supported")
		}
		c.Transport = transport
	}
	resp, err := c.Do(req)
	if err != nil {
		if r.isDoH3 {
			if closer, ok := c.Transport.(io.Closer); ok {
				closer.Close()
			}
		}
		return nil, fmt.Errorf("could not perform request: %w", err)
	}
	defer resp.Body.Close()

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read message from response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wrong response from DOH server, got: %s, status: %d", string(buf), resp.StatusCode)
	}

	answer := new(dns.Msg)
	if err := answer.Unpack(buf); err != nil {
		return nil, fmt.Errorf("answer.Unpack: %w", err)
	}
	return answer, nil
}

func addHeader(ctx context.Context, req *http.Request, sendClientInfo bool) {
	req.Header.Set("Content-Type", headerApplicationDNS)
	req.Header.Set("Accept", headerApplicationDNS)
	req.Header.Set(dohOsHeader, dohOsHeaderValue())

	printed := false
	if sendClientInfo {
		if ci, ok := ctx.Value(ClientInfoCtxKey{}).(*ClientInfo); ok && ci != nil {
			printed = ci.Mac != "" || ci.IP != "" || ci.Hostname != ""
			if ci.Mac != "" {
				req.Header.Set(dohMacHeader, ci.Mac)
			}
			if ci.IP != "" {
				req.Header.Set(dohIPHeader, ci.IP)
			}
			if ci.Hostname != "" {
				req.Header.Set(dohHostHeader, ci.Hostname)
			}
			if ci.Self {
				req.Header.Set(dohOsHeader, dohOsHeaderValue())
			}
		}
	}
	if printed {
		Log(ctx, ProxyLogger.Load().Debug().Interface("header", req.Header), "sending request header")
	}
}
