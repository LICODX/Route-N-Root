package network

import (
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

type IPTracker struct {
	peerIPs map[peer.ID]string
}

func NewIPTracker() *IPTracker {
	return &IPTracker{
		peerIPs: make(map[peer.ID]string),
	}
}

func (ipt *IPTracker) TrackPeerIP(peerID peer.ID, addrs []multiaddr.Multiaddr) string {
	for _, addr := range addrs {
		ip := extractIPFromMultiaddr(addr)
		if ip != "" {
			ipt.peerIPs[peerID] = ip
			return ip
		}
	}
	return ""
}

func (ipt *IPTracker) GetPeerIP(peerID peer.ID) string {
	return ipt.peerIPs[peerID]
}

func extractIPFromMultiaddr(maddr multiaddr.Multiaddr) string {
	parts := strings.Split(maddr.String(), "/")
	
	for i, part := range parts {
		if part == "ip4" || part == "ip6" {
			if i+1 < len(parts) {
				return parts[i+1]
			}
		}
	}
	
	return ""
}

func GetSubnet24(ip string) string {
	parts := strings.Split(ip, ".")
	if len(parts) >= 3 {
		return parts[0] + "." + parts[1] + "." + parts[2] + ".0/24"
	}
	return "unknown"
}
