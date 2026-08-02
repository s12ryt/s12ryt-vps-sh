package main

import (
	"context"
	"crypto/rand"
	"log"
	"net/http"
	"time"

	"github.com/s12ryt/s12ryt-vps-sh/internal/auth"
	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/panel"
	projectqrcode "github.com/s12ryt/s12ryt-vps-sh/internal/qrcode"
	"github.com/s12ryt/s12ryt-vps-sh/internal/share"
)

const fixtureAddress = "127.0.0.1:18080"

type fixtureShareService struct {
	bundle share.Bundle
}

func (service fixtureShareService) Bundle(context.Context) (share.Bundle, error) {
	return service.bundle, nil
}

func main() {
	hasher := auth.NewPasswordHasher(rand.Reader)
	passwordHash, err := hasher.Hash("panel-password")
	if err != nil {
		log.Fatal(err)
	}
	renderer, err := projectqrcode.NewPNGRenderer()
	if err != nil {
		log.Fatal(err)
	}
	uri := "vless://11111111-1111-4111-8111-111111111111@[2001:db8::10]:24443?security=tls&sni=node.example.com#edge-v6"
	qrPNG, err := renderer.RenderPNG(uri)
	if err != nil {
		log.Fatal(err)
	}
	server := panel.NewServer(panel.Options{
		BasePath:     "/abcdefghijkl",
		PasswordHash: passwordHash,
		Hasher:       hasher,
		Sessions:     auth.NewSessionManager(rand.Reader, time.Now),
		Limiter:      auth.NewLoginLimiter(time.Now),
		Config:       domain.DefaultConfig(),
		ShareService: fixtureShareService{bundle: share.Bundle{
			Nodes: []share.Artifact{{
				NodeID:              "edge-v6",
				URI:                 uri,
				QRPayload:           uri,
				QRPNG:               qrPNG,
				ClientJSON:          []byte(`{"outbounds":[{"type":"vless","tag":"proxy"}]}`),
				FullClientJSON:      []byte(`{"route":{"rules":[{"ip_version":4,"outbound":"direct"}]}}`),
				FullClientBase64:    "eyJyb3V0ZSI6eyJydWxlcyI6W3siaXBfdmVyc2lvbiI6NCwib3V0Ym91bmQiOiJkaXJlY3QifV19fQ==",
				SplitRoutingWarning: "URI 與 QR 不包含分流規則。",
			}},
			Subscription: "dmxlc3M6Ly8xMTExMTExMS0xMTExLTQxMTEtODExMS0xMTExMTExMTExMTFAMjAwMTpkYjg6OjEwOjI0NDQz",
		}},
	})
	httpServer := &http.Server{
		Addr:              fixtureAddress,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("Playwright fixture listening on %s", fixtureAddress)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

var _ panel.ShareService = fixtureShareService{}
