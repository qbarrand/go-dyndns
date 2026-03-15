package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/hashicorp/go-set/v3"
	"github.com/miekg/dns"
)

var (
	version string
	commit  string
	date    string
)

type Updater struct {
	username string
	password string
	hostname string
}

func NewUpdater(username, password, hostname string) *Updater {
	return &Updater{
		username: username,
		password: password,
		hostname: hostname,
	}
}

func (u *Updater) Update(hostname string, address netip.Addr) error {
	req, err := http.NewRequest(http.MethodGet, u.hostname, nil)
	if err != nil {
		return err
	}

	req.SetBasicAuth(u.username, u.password)

	q := req.URL.Query()
	q.Add("system", "dyndns")
	q.Add("hostname", hostname)
	q.Add("myip", address.String())

	req.URL.RawQuery = q.Encode()

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("dyndns server replied %s", res.Status)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("could not read the response body: %v", err)
	}

	bodyStr := strings.Split(string(body), " ")

	if len(bodyStr) < 1 || bodyStr[0] != "good" {
		return fmt.Errorf("response body: %s", bodyStr)
	}

	return nil
}

func main() {
	configFile := flag.String(
		"config",
		"/etc/go-dyndns.toml",
		"path to the configuration file to use")

	dryRun := flag.Bool(
		"dry",
		false,
		"do not actually configure the new DynHost")

	showVersion := flag.Bool(
		"version",
		false,
		"show the version of this software")

	flag.Parse()

	if *showVersion {
		fmt.Printf("go-dyndns %s (commit %s, date %s)\n", version, commit, date)
		return
	}

	log.Printf("Reading configuration file at %s", *configFile)

	cfg, err := ReadFromFile(*configFile)
	if err != nil {
		log.Fatalf("Could not read the configuration file: %v", err)
	}

	updater := NewUpdater(cfg.Provider.Username, cfg.Provider.Password, cfg.Provider.Hostname)

	log.Printf("Config file has %d domain configs", len(cfg.Domain))

	var ipv4, ipv6, zero netip.Addr

	for domain, domainConfig := range cfg.Domain {
		log.Printf("Processing domain %s (IPv4: %t, IPv6: %t)", domain, domainConfig.IPv4, domainConfig.IPv6)

		if !domainConfig.IPv4 && !domainConfig.IPv6 {
			log.Printf("No IP version enabled for domain %s; skipping", domain)
			continue
		}

		existingIPs, err := getDynHostValue(cfg.General.Resolver, domain)
		if err != nil {
			log.Printf("Could not get the current IPs for %s from DNS: %v; skipping", domain, err)
			continue
		}

		log.Printf("Current IPs for %s: %v", domain, existingIPs)

		update := func(publicIP netip.Addr) error {
			if existingIPs.Contains(publicIP) {
				log.Printf("The current IP for %s is already %s; skipping", domain, publicIP.String())
				return nil
			}

			log.Printf("Updating the record for %s to %s", domain, publicIP.String())

			if *dryRun {
				log.Printf("Dry run; skipping the update")
				return nil
			}

			if err := updater.Update(domain, publicIP); err != nil {
				return fmt.Errorf("could not update the DynHost record for %s: %v", domain, err)
			}

			return nil
		}

		if domainConfig.IPv4 {
			if ipv4 == zero {
				ipv4, err = getPublicIPv4()
				if err != nil {
					log.Fatalf("Could not get public IPv4 address for %s: %v; skipping IPv4 update", domain, err)
				}
			}

			update(ipv4)
		}

		if domainConfig.IPv6 {
			if ipv6 == zero {
				ipv6, err = getPublicIPv6()
				if err != nil {
					log.Fatalf("Could not get public IPv6 address for %s: %v; skipping IPv6 update", domain, err)
				}
			}

			update(ipv6)
		}
	}
}

func getDynHostValue(resolver, hostname string) (*set.Set[netip.Addr], error) {
	s := set.New[netip.Addr](2)

	questionName := hostname + "."

	m := new(dns.Msg)
	m.SetQuestion(questionName, dns.TypeA)

	in, err := dns.Exchange(m, resolver)
	if err != nil {
		return s, fmt.Errorf("could not query DNS for %s: %v", hostname, err)
	}

	if in.Rcode != dns.RcodeSuccess {
		return s, fmt.Errorf("DNS query for %s failed with code %d", hostname, in.Rcode)
	}

	log.Printf("Getting A records for %s", hostname)

	for _, rr := range in.Answer {
		if a, ok := rr.(*dns.A); ok {
			ip, ok := netip.AddrFromSlice(a.A)
			if !ok {
				log.Printf("Could not parse %s as an IP address; skipping", a.A.String())
				continue
			}

			s.Insert(ip)
		}
	}

	m = new(dns.Msg)
	m.SetQuestion(questionName, dns.TypeAAAA)

	in, err = dns.Exchange(m, resolver)
	if err != nil {
		return s, fmt.Errorf("could not query DNS for %s: %v", hostname, err)
	}

	if in.Rcode != dns.RcodeSuccess {
		return s, fmt.Errorf("DNS query for %s failed with code %d", hostname, in.Rcode)
	}

	log.Printf("Getting AAAA records for %s", hostname)

	for _, rr := range in.Answer {
		if aaaa, ok := rr.(*dns.AAAA); ok {
			ip, ok := netip.AddrFromSlice(aaaa.AAAA)
			if !ok {
				log.Printf("Could not parse %s as an IP address; skipping", aaaa.AAAA.String())
				continue
			}

			s.Insert(ip)
		}
	}

	return s, nil
}

func getPublicIPv4() (netip.Addr, error) {
	res, err := http.Get("https://api.ipify.org")
	if err != nil {
		return netip.Addr{}, err
	}
	defer res.Body.Close()

	resCode := res.StatusCode

	if resCode != http.StatusOK {
		return netip.Addr{}, fmt.Errorf("returned %d", resCode)
	}

	ipStrBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("could not read the response: %v", err)
	}

	return netip.ParseAddr(string(ipStrBytes))
}

var interfaceAddrsGetter = net.InterfaceAddrs

var globalIPv6Net = netip.MustParsePrefix("2000::/3")

func getPublicIPv6() (netip.Addr, error) {
	addrs, err := interfaceAddrsGetter()
	if err != nil {
		return netip.Addr{}, fmt.Errorf("could not list local addresses: %v", err)
	}

	for _, a := range addrs {
		addr_str := a.String()

		ip, err := netip.ParsePrefix(addr_str)
		if err != nil {
			log.Printf("Could not parse %s as an IP address; skipping", addr_str)
			continue
		}

		if addr := ip.Addr(); globalIPv6Net.Contains(addr) {
			return addr, nil
		}
	}

	return netip.Addr{}, errors.New("no public IPv6 address found")
}
