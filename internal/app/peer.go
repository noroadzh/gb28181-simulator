// Package app — peer matching helpers.
package app

import "net"

// samePeer reports whether peer and server refer to the same network endpoint
// for SIP transaction response filtering. It uses address-semantic equivalence
// rather than string equality so that a configured hostname (e.g. a Docker
// service name) can match a real UDP source address (always an IP).
func samePeer(peer, server string) bool {
	peerHost, peerPort, errP := net.SplitHostPort(peer)
	serverHost, serverPort, errS := net.SplitHostPort(server)
	if errP != nil || errS != nil {
		return peer == server
	}
	if peerPort != serverPort {
		return false
	}
	peerIP := net.ParseIP(peerHost)
	serverIP := net.ParseIP(serverHost)
	switch {
	case peerIP != nil && serverIP != nil:
		return peerIP.Equal(serverIP)
	case peerHost == serverHost:
		return true
	case peerIP != nil:
		ips, err := net.LookupHost(serverHost)
		if err != nil {
			return false
		}
		return containsIP(ips, peerIP)
	case serverIP != nil:
		ips, err := net.LookupHost(peerHost)
		if err != nil {
			return false
		}
		return containsIP(ips, serverIP)
	default:
		serverIPs, errS := net.LookupHost(serverHost)
		peerIPs, errP := net.LookupHost(peerHost)
		if errS != nil || errP != nil {
			return false
		}
		return intersectIPs(peerIPs, serverIPs)
	}
}

func containsIP(list []string, target net.IP) bool {
	for _, s := range list {
		if net.ParseIP(s).Equal(target) {
			return true
		}
	}
	return false
}

func intersectIPs(a, b []string) bool {
	set := make(map[string]struct{}, len(a))
	for _, s := range a {
		set[s] = struct{}{}
	}
	for _, s := range b {
		if _, ok := set[s]; ok {
			return true
		}
	}
	return false
}
