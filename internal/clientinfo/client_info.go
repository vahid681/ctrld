package clientinfo

import (
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/Control-D-Inc/ctrld"
	"github.com/Control-D-Inc/ctrld/internal/controld"
)

// IpResolver is the interface for retrieving IP from Mac.
type IpResolver interface {
	fmt.Stringer
	// LookupIP returns ip of the device with given mac.
	LookupIP(mac string) string
}

// MacResolver is the interface for retrieving Mac from IP.
type MacResolver interface {
	fmt.Stringer
	// LookupMac returns mac of the device with given ip.
	LookupMac(ip string) string
}

// HostnameByIpResolver is the interface for retrieving hostname from IP.
type HostnameByIpResolver interface {
	// LookupHostnameByIP returns hostname of the given ip.
	LookupHostnameByIP(ip string) string
}

// HostnameByMacResolver is the interface for retrieving hostname from Mac.
type HostnameByMacResolver interface {
	// LookupHostnameByMac returns hostname of the device with given mac.
	LookupHostnameByMac(mac string) string
}

// HostnameResolver is the interface for retrieving hostname from either IP or Mac.
type HostnameResolver interface {
	fmt.Stringer
	HostnameByIpResolver
	HostnameByMacResolver
}

type refresher interface {
	refresh() error
}

type ipLister interface {
	fmt.Stringer
	// List returns list of ip known by the resolver.
	List() []string
}

type Client struct {
	IP       netip.Addr
	Mac      string
	Hostname string
	Source   map[string]struct{}
}

type Table struct {
	ipResolvers       []IpResolver
	macResolvers      []MacResolver
	hostnameResolvers []HostnameResolver
	refreshers        []refresher
	initOnce          sync.Once

	dhcp   *dhcp
	merlin *merlinDiscover
	arp    *arpDiscover
	ptr    *ptrDiscover
	mdns   *mdns
	hf     *hostsFile
	vni    *virtualNetworkIface
	cfg    *ctrld.Config
	quitCh chan struct{}
	selfIP string
	cdUID  string
}

func NewTable(cfg *ctrld.Config, selfIP, cdUID string) *Table {
	return &Table{
		cfg:    cfg,
		quitCh: make(chan struct{}),
		selfIP: selfIP,
		cdUID:  cdUID,
	}
}

func (t *Table) AddLeaseFile(name string, format ctrld.LeaseFileFormat) {
	if !t.discoverDHCP() {
		return
	}
	clientInfoFiles[name] = format
}

func (t *Table) RefreshLoop(stopCh chan struct{}) {
	timer := time.NewTicker(time.Minute * 5)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			for _, r := range t.refreshers {
				_ = r.refresh()
			}
		case <-stopCh:
			close(t.quitCh)
			return
		}
	}
}

func (t *Table) Init() {
	t.initOnce.Do(t.init)
}

func (t *Table) init() {
	// Custom client ID presents, use it as the only source.
	if _, clientID := controld.ParseRawUID(t.cdUID); clientID != "" {
		ctrld.ProxyLogger.Load().Debug().Msg("start self discovery")
		t.dhcp = &dhcp{selfIP: t.selfIP}
		t.dhcp.addSelf()
		t.ipResolvers = append(t.ipResolvers, t.dhcp)
		t.macResolvers = append(t.macResolvers, t.dhcp)
		t.hostnameResolvers = append(t.hostnameResolvers, t.dhcp)
		return
	}

	// Otherwise, process all possible sources in order, that means
	// the first result of IP/MAC/Hostname lookup will be used.
	//
	// Merlin custom clients.
	if t.discoverDHCP() || t.discoverARP() {
		t.merlin = &merlinDiscover{}
		if err := t.merlin.refresh(); err != nil {
			ctrld.ProxyLogger.Load().Error().Err(err).Msg("could not init Merlin discover")
		} else {
			t.hostnameResolvers = append(t.hostnameResolvers, t.merlin)
			t.refreshers = append(t.refreshers, t.merlin)
		}
	}
	// Hosts file mapping.
	if t.discoverHosts() {
		t.hf = &hostsFile{}
		ctrld.ProxyLogger.Load().Debug().Msg("start hosts file discovery")
		if err := t.hf.init(); err != nil {
			ctrld.ProxyLogger.Load().Error().Err(err).Msg("could not init hosts file discover")
		} else {
			t.hostnameResolvers = append(t.hostnameResolvers, t.hf)
			t.refreshers = append(t.refreshers, t.hf)
		}
		go t.hf.watchChanges()
	}
	// DHCP lease files.
	if t.discoverDHCP() {
		t.dhcp = &dhcp{selfIP: t.selfIP}
		ctrld.ProxyLogger.Load().Debug().Msg("start dhcp discovery")
		if err := t.dhcp.init(); err != nil {
			ctrld.ProxyLogger.Load().Error().Err(err).Msg("could not init DHCP discover")
		} else {
			t.ipResolvers = append(t.ipResolvers, t.dhcp)
			t.macResolvers = append(t.macResolvers, t.dhcp)
			t.hostnameResolvers = append(t.hostnameResolvers, t.dhcp)
		}
		go t.dhcp.watchChanges()
	}
	// ARP table.
	if t.discoverARP() {
		t.arp = &arpDiscover{}
		ctrld.ProxyLogger.Load().Debug().Msg("start arp discovery")
		if err := t.arp.refresh(); err != nil {
			ctrld.ProxyLogger.Load().Error().Err(err).Msg("could not init ARP discover")
		} else {
			t.ipResolvers = append(t.ipResolvers, t.arp)
			t.macResolvers = append(t.macResolvers, t.arp)
			t.refreshers = append(t.refreshers, t.arp)
		}
	}
	// PTR lookup.
	if t.discoverPTR() {
		t.ptr = &ptrDiscover{resolver: ctrld.NewPrivateResolver()}
		ctrld.ProxyLogger.Load().Debug().Msg("start ptr discovery")
		if err := t.ptr.refresh(); err != nil {
			ctrld.ProxyLogger.Load().Error().Err(err).Msg("could not init PTR discover")
		} else {
			t.hostnameResolvers = append(t.hostnameResolvers, t.ptr)
			t.refreshers = append(t.refreshers, t.ptr)
		}
	}
	// mdns.
	if t.discoverMDNS() {
		t.mdns = &mdns{}
		ctrld.ProxyLogger.Load().Debug().Msg("start mdns discovery")
		if err := t.mdns.init(t.quitCh); err != nil {
			ctrld.ProxyLogger.Load().Error().Err(err).Msg("could not init mDNS discover")
		} else {
			t.hostnameResolvers = append(t.hostnameResolvers, t.mdns)
		}
	}
	// VPN clients.
	if t.discoverDHCP() || t.discoverARP() {
		t.vni = &virtualNetworkIface{}
		t.hostnameResolvers = append(t.hostnameResolvers, t.vni)
	}
}

func (t *Table) LookupIP(mac string) string {
	t.initOnce.Do(t.init)
	for _, r := range t.ipResolvers {
		if ip := r.LookupIP(mac); ip != "" {
			return ip
		}
	}
	return ""
}

func (t *Table) LookupMac(ip string) string {
	t.initOnce.Do(t.init)
	for _, r := range t.macResolvers {
		if mac := r.LookupMac(ip); mac != "" {
			return mac
		}
	}
	return ""
}

func (t *Table) LookupHostname(ip, mac string) string {
	t.initOnce.Do(t.init)
	for _, r := range t.hostnameResolvers {
		if name := r.LookupHostnameByIP(ip); name != "" {
			return name
		}
		if name := r.LookupHostnameByMac(mac); name != "" {
			return name
		}
	}
	return ""
}

type macEntry struct {
	mac string
	src string
}

type hostnameEntry struct {
	name string
	src  string
}

func (t *Table) lookupMacAll(ip string) []*macEntry {
	var res []*macEntry
	for _, r := range t.macResolvers {
		res = append(res, &macEntry{mac: r.LookupMac(ip), src: r.String()})
	}
	return res
}

func (t *Table) lookupHostnameAll(ip, mac string) []*hostnameEntry {
	var res []*hostnameEntry
	for _, r := range t.hostnameResolvers {
		src := r.String()
		// For ptrDiscover, lookup hostname may block due to server unavailable,
		// so only lookup from cache to prevent timeout reached.
		if ptrResolver, ok := r.(*ptrDiscover); ok {
			if name := ptrResolver.lookupHostnameFromCache(ip); name != "" {
				res = append(res, &hostnameEntry{name: name, src: src})
			}
			continue
		}
		if name := r.LookupHostnameByIP(ip); name != "" {
			res = append(res, &hostnameEntry{name: name, src: src})
			continue
		}
		if name := r.LookupHostnameByMac(mac); name != "" {
			res = append(res, &hostnameEntry{name: name, src: src})
			continue
		}
	}
	return res
}

// ListClients returns list of clients discovered by ctrld.
func (t *Table) ListClients() []*Client {
	for _, r := range t.refreshers {
		_ = r.refresh()
	}
	ipMap := make(map[string]*Client)
	il := []ipLister{t.dhcp, t.arp, t.ptr, t.mdns, t.vni}
	for _, ir := range il {
		for _, ip := range ir.List() {
			c, ok := ipMap[ip]
			if !ok {
				c = &Client{
					IP:     netip.MustParseAddr(ip),
					Source: map[string]struct{}{ir.String(): {}},
				}
				ipMap[ip] = c
			} else {
				c.Source[ir.String()] = struct{}{}
			}
		}
	}
	for ip := range ipMap {
		c := ipMap[ip]
		for _, e := range t.lookupMacAll(ip) {
			if c.Mac == "" && e.mac != "" {
				c.Mac = e.mac
			}
			if e.mac != "" {
				c.Source[e.src] = struct{}{}
			}
		}
		for _, e := range t.lookupHostnameAll(ip, c.Mac) {
			if c.Hostname == "" && e.name != "" {
				c.Hostname = e.name
			}
			if e.name != "" {
				c.Source[e.src] = struct{}{}
			}
		}
	}
	clients := make([]*Client, 0, len(ipMap))
	for _, c := range ipMap {
		clients = append(clients, c)
	}
	return clients
}

// StoreVPNClient stores client info for VPN clients.
func (t *Table) StoreVPNClient(ci *ctrld.ClientInfo) {
	if ci == nil || t.vni == nil {
		return
	}
	t.vni.mac.Store(ci.IP, ci.Mac)
	t.vni.ip2name.Store(ci.IP, ci.Hostname)
}

func (t *Table) discoverDHCP() bool {
	if t.cfg.Service.DiscoverDHCP == nil {
		return true
	}
	return *t.cfg.Service.DiscoverDHCP
}

func (t *Table) discoverARP() bool {
	if t.cfg.Service.DiscoverARP == nil {
		return true
	}
	return *t.cfg.Service.DiscoverARP
}

func (t *Table) discoverMDNS() bool {
	if t.cfg.Service.DiscoverMDNS == nil {
		return true
	}
	return *t.cfg.Service.DiscoverMDNS
}

func (t *Table) discoverPTR() bool {
	if t.cfg.Service.DiscoverPtr == nil {
		return true
	}
	return *t.cfg.Service.DiscoverPtr
}

func (t *Table) discoverHosts() bool {
	if t.cfg.Service.DiscoverHosts == nil {
		return true
	}
	return *t.cfg.Service.DiscoverHosts
}

// normalizeIP normalizes the ip parsed from dnsmasq/dhcpd lease file.
func normalizeIP(in string) string {
	// dnsmasq may put ip with interface index in lease file, strip it here.
	ip, _, found := strings.Cut(in, "%")
	if found {
		return ip
	}
	return in
}

func normalizeHostname(name string) string {
	if before, _, found := strings.Cut(name, "."); found {
		return before // remove ".local.", ".lan.", ... suffix
	}
	return name
}
