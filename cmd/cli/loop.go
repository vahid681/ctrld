package cli

import (
	"context"
	"strings"
	"time"

	"github.com/miekg/dns"

	"github.com/Control-D-Inc/ctrld"
)

const (
	loopTestDomain = ".test"
	loopTestQtype  = dns.TypeTXT
)

// isLoop reports whether the given upstream config is detected as having DNS loop.
func (p *prog) isLoop(uc *ctrld.UpstreamConfig) bool {
	p.loopMu.Lock()
	defer p.loopMu.Unlock()
	return p.loop[uc.UID()]
}

// detectLoop checks if the given DNS message is initialized sent by ctrld.
// If yes, marking the corresponding upstream as loop, prevent infinite DNS
// forwarding loop.
//
// See p.checkDnsLoop for more details how it works.
func (p *prog) detectLoop(msg *dns.Msg) {
	if len(msg.Question) != 1 {
		return
	}
	q := msg.Question[0]
	if q.Qtype != loopTestQtype {
		return
	}
	unFQDNname := strings.TrimSuffix(q.Name, ".")
	uid := strings.TrimSuffix(unFQDNname, loopTestDomain)
	p.loopMu.Lock()
	if _, loop := p.loop[uid]; loop {
		p.loop[uid] = loop
	}
	p.loopMu.Unlock()
}

// checkDnsLoop sends a message to check if there's any DNS forwarding loop
// with all the upstreams. The way it works based on dnsmasq --dns-loop-detect.
//
// - Generating a TXT test query and sending it to all upstream.
// - The test query is formed by upstream UID and test domain: <uid>.test
// - If the test query returns to ctrld, mark the corresponding upstream as loop (see p.detectLoop).
//
// See: https://thekelleys.org.uk/dnsmasq/docs/dnsmasq-man.html
func (p *prog) checkDnsLoop() {
	mainLog.Load().Debug().Msg("start checking DNS loop")
	upstream := make(map[string]*ctrld.UpstreamConfig)
	p.loopMu.Lock()
	for _, uc := range p.cfg.Upstream {
		uid := uc.UID()
		p.loop[uid] = false
		upstream[uid] = uc
	}
	p.loopMu.Unlock()

	for uid := range p.loop {
		msg := loopTestMsg(uid)
		uc := upstream[uid]
		resolver, err := ctrld.NewResolver(uc)
		if err != nil {
			mainLog.Load().Warn().Err(err).Msgf("could not perform loop check for upstream: %q, endpoint: %q", uc.Name, uc.Endpoint)
			continue
		}
		if _, err := resolver.Resolve(context.Background(), msg); err != nil {
			mainLog.Load().Warn().Err(err).Msgf("could not send DNS loop check query for upstream: %q, endpoint: %q", uc.Name, uc.Endpoint)
		}
	}
	mainLog.Load().Debug().Msg("end checking DNS loop")
}

// checkDnsLoopTicker performs p.checkDnsLoop every minute.
func (p *prog) checkDnsLoopTicker() {
	timer := time.NewTicker(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-timer.C:
			p.checkDnsLoop()
		}
	}
}

// loopTestMsg generates DNS message for checking loop.
func loopTestMsg(uid string) *dns.Msg {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(uid+loopTestDomain), loopTestQtype)
	return msg
}
