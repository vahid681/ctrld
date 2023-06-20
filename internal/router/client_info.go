package router

import (
	"bufio"
	"bytes"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"tailscale.com/util/lineread"

	"github.com/Control-D-Inc/ctrld"
)

// readClientInfoFunc represents the function for reading client info.
type readClientInfoFunc func(name string) error

// clientInfoFiles specifies client info files and how to read them on supported platforms.
var clientInfoFiles = map[string]readClientInfoFunc{
	"/tmp/dnsmasq.leases":                      dnsmasqReadClientInfoFile, // ddwrt
	"/tmp/dhcp.leases":                         dnsmasqReadClientInfoFile, // openwrt
	"/var/lib/misc/dnsmasq.leases":             dnsmasqReadClientInfoFile, // merlin
	"/mnt/data/udapi-config/dnsmasq.lease":     dnsmasqReadClientInfoFile, // UDM Pro
	"/data/udapi-config/dnsmasq.lease":         dnsmasqReadClientInfoFile, // UDR
	"/etc/dhcpd/dhcpd-leases.log":              dnsmasqReadClientInfoFile, // Synology
	"/tmp/var/lib/misc/dnsmasq.leases":         dnsmasqReadClientInfoFile, // Tomato
	"/run/dnsmasq-dhcp.leases":                 dnsmasqReadClientInfoFile, // EdgeOS
	"/run/dhcpd.leases":                        iscDHCPReadClientInfoFile, // EdgeOS
	"/var/dhcpd/var/db/dhcpd.leases":           iscDHCPReadClientInfoFile, // Pfsense
	"/home/pi/.router/run/dhcp/dnsmasq.leases": dnsmasqReadClientInfoFile, // Firewalla
}

// watchClientInfoTable watches changes happens in dnsmasq/dhcpd
// lease files, perform updating to mac table if necessary.
func (r *router) watchClientInfoTable() {
	if r.watcher == nil {
		return
	}
	timer := time.NewTicker(time.Minute * 5)
	for {
		select {
		case <-timer.C:
			for _, name := range r.watcher.WatchList() {
				_ = clientInfoFiles[name](name)
			}
		case event, ok := <-r.watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) {
				readFunc := clientInfoFiles[event.Name]
				if readFunc == nil {
					log.Println("unknown file format:", event.Name)
					continue
				}
				if err := readFunc(event.Name); err != nil && !os.IsNotExist(err) {
					log.Println("could not read client info file:", err)
				}
			}
		case err, ok := <-r.watcher.Errors:
			if !ok {
				return
			}
			log.Println("error:", err)
		}
	}
}

// Stop performs tasks need to be done before the router stopped.
func Stop() error {
	if Name() == "" {
		return nil
	}
	r := routerPlatform.Load()
	if r.watcher != nil {
		if err := r.watcher.Close(); err != nil {
			return err
		}
	}
	return nil
}

// GetClientInfoByMac returns ClientInfo for the client associated with the given mac.
func GetClientInfoByMac(mac string) *ctrld.ClientInfo {
	if mac == "" {
		return nil
	}
	_ = Name()
	r := routerPlatform.Load()
	val, ok := r.mac.Load(mac)
	if !ok {
		return nil
	}
	return val.(*ctrld.ClientInfo)
}

// dnsmasqReadClientInfoFile populates mac table with client info reading from dnsmasq lease file.
func dnsmasqReadClientInfoFile(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return dnsmasqReadClientInfoReader(f)

}

// dnsmasqReadClientInfoReader likes dnsmasqReadClientInfoFile, but reading from an io.Reader instead of file.
func dnsmasqReadClientInfoReader(reader io.Reader) error {
	r := routerPlatform.Load()
	return lineread.Reader(reader, func(line []byte) error {
		fields := bytes.Fields(line)
		if len(fields) < 4 {
			return nil
		}
		mac := string(fields[1])
		if _, err := net.ParseMAC(mac); err != nil {
			// The second field is not a mac, skip.
			return nil
		}
		ip := normalizeIP(string(fields[2]))
		if net.ParseIP(ip) == nil {
			log.Printf("invalid ip address entry: %q", ip)
			ip = ""
		}
		hostname := string(fields[3])
		r.mac.Store(mac, &ctrld.ClientInfo{Mac: mac, IP: ip, Hostname: hostname})
		return nil
	})
}

// iscDHCPReadClientInfoFile populates mac table with client info reading from isc-dhcpd lease file.
func iscDHCPReadClientInfoFile(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return iscDHCPReadClientInfoReader(f)
}

// iscDHCPReadClientInfoReader likes iscDHCPReadClientInfoFile, but reading from an io.Reader instead of file.
func iscDHCPReadClientInfoReader(reader io.Reader) error {
	r := routerPlatform.Load()
	s := bufio.NewScanner(reader)
	var ip, mac, hostname string
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "}") {
			if mac != "" {
				r.mac.Store(mac, &ctrld.ClientInfo{Mac: mac, IP: ip, Hostname: hostname})
				ip, mac, hostname = "", "", ""
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "lease":
			ip = normalizeIP(strings.ToLower(fields[1]))
			if net.ParseIP(ip) == nil {
				log.Printf("invalid ip address entry: %q", ip)
				ip = ""
			}
		case "hardware":
			if len(fields) >= 3 {
				mac = strings.ToLower(strings.TrimRight(fields[2], ";"))
				if _, err := net.ParseMAC(mac); err != nil {
					// Invalid mac, skip.
					mac = ""
				}
			}
		case "client-hostname":
			hostname = strings.Trim(fields[1], `";`)
		}
	}
	return nil
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
