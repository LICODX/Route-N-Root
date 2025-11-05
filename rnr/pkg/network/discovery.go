package network

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/multiformats/go-multiaddr"
)

var BootstrapPeers = []string{
	"/ip4/104.131.131.82/tcp/4001/p2p/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN",
}

type PeerDiscovery struct {
	host       host.Host
	ctx        context.Context
	dht        *dht.IpfsDHT
	mdns       mdns.Service
	routingDis *drouting.RoutingDiscovery
	peers      map[peer.ID]bool
	mu         sync.RWMutex
}

func NewPeerDiscovery(ctx context.Context, h host.Host) (*PeerDiscovery, error) {
	kdht, err := dht.New(ctx, h, dht.Mode(dht.ModeAutoServer))
	if err != nil {
		return nil, fmt.Errorf("failed to create DHT: %w", err)
	}

	if err := kdht.Bootstrap(ctx); err != nil {
		return nil, fmt.Errorf("failed to bootstrap DHT: %w", err)
	}

	routingDiscovery := routing.NewRoutingDiscovery(kdht)

	pd := &PeerDiscovery{
		host:       h,
		ctx:        ctx,
		dht:        kdht,
		routingDis: routingDiscovery,
		peers:      make(map[peer.ID]bool),
	}

	if err := pd.setupMDNS(); err != nil {
		log.Printf("⚠️  MDNS setup failed: %v", err)
	}

	go pd.connectToBootstrap()
	go pd.discoverPeers()

	return pd, nil
}

func (pd *PeerDiscovery) setupMDNS() error {
	s := mdns.NewMdnsService(pd.host, "rnr-blockchain", pd)
	pd.mdns = s
	return s.Start()
}

func (pd *PeerDiscovery) HandlePeerFound(pi peer.AddrInfo) {
	pd.mu.Lock()
	if pd.peers[pi.ID] {
		pd.mu.Unlock()
		return
	}
	pd.peers[pi.ID] = true
	pd.mu.Unlock()

	if err := pd.host.Connect(pd.ctx, pi); err != nil {
		log.Printf("⚠️  Failed to connect to peer %s: %v", pi.ID.String()[:12], err)
		return
	}

	log.Printf("🔗 Discovered and connected to peer: %s", pi.ID.String()[:12])
}

func (pd *PeerDiscovery) connectToBootstrap() {
	for _, peerAddr := range BootstrapPeers {
		maddr, err := multiaddr.NewMultiaddr(peerAddr)
		if err != nil {
			continue
		}

		peerInfo, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			continue
		}

		if err := pd.host.Connect(pd.ctx, *peerInfo); err != nil {
			log.Printf("⚠️  Failed to connect to bootstrap peer: %v", err)
			continue
		}

		log.Printf("✅ Connected to bootstrap peer: %s", peerInfo.ID.String()[:12])
	}
}

func (pd *PeerDiscovery) discoverPeers() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-pd.ctx.Done():
			return
		case <-ticker.C:
			pd.advertise()
			pd.findPeers()
		}
	}
}

func (pd *PeerDiscovery) advertise() {
	_, err := pd.routingDis.Advertise(pd.ctx, "rnr-blockchain")
	if err != nil {
		log.Printf("⚠️  Advertisement failed: %v", err)
	}
}

func (pd *PeerDiscovery) findPeers() {
	peerChan, err := pd.routingDis.FindPeers(pd.ctx, "rnr-blockchain")
	if err != nil {
		log.Printf("⚠️  Peer discovery failed: %v", err)
		return
	}

	for peer := range peerChan {
		if peer.ID == pd.host.ID() {
			continue
		}

		pd.HandlePeerFound(peer)
	}
}

func (pd *PeerDiscovery) GetPeers() []peer.ID {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	peers := make([]peer.ID, 0, len(pd.peers))
	for id := range pd.peers {
		peers = append(peers, id)
	}
	return peers
}

func (pd *PeerDiscovery) GetPeerCount() int {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	return len(pd.peers)
}

func (pd *PeerDiscovery) Close() error {
	if pd.mdns != nil {
		pd.mdns.Close()
	}
	return pd.dht.Close()
}
