package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"go.etcd.io/etcd/server/v3/embed"
)

func main() {
	var dataDir string
	var clientAddr string
	var peerAddr string
	flag.StringVar(&dataDir, "data-dir", filepath.Join("example", "data", "etcd"), "etcd data dir")
	flag.StringVar(&clientAddr, "client", "http://127.0.0.1:2379", "client listen/advertise url")
	flag.StringVar(&peerAddr, "peer", "http://127.0.0.1:2380", "peer listen/advertise url")
	flag.Parse()

	cfg := embed.NewConfig()
	cfg.Dir = dataDir
	cfg.LogLevel = "warn"

	cu, err := url.Parse(clientAddr)
	if err != nil {
		log.Fatalf("invalid --client: %v", err)
	}
	pu, err := url.Parse(peerAddr)
	if err != nil {
		log.Fatalf("invalid --peer: %v", err)
	}

	cfg.ListenClientUrls = []url.URL{*cu}
	cfg.AdvertiseClientUrls = []url.URL{*cu}
	cfg.ListenPeerUrls = []url.URL{*pu}
	cfg.AdvertisePeerUrls = []url.URL{*pu}
	cfg.InitialCluster = fmt.Sprintf("default=%s", pu.String())
	cfg.InitialClusterToken = "emberengine-local"
	cfg.ClusterState = "new"

	e, err := embed.StartEtcd(cfg)
	if err != nil {
		log.Fatalf("start etcd failed: %v", err)
	}
	defer e.Close()

	select {
	case <-e.Server.ReadyNotify():
		log.Printf("etcd ready: client=%s peer=%s dir=%s", clientAddr, peerAddr, dataDir)
	case <-time.After(30 * time.Second):
		e.Server.Stop() // trigger shutdown
		log.Fatalf("etcd startup timed out")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	log.Printf("etcd shutting down")
}
