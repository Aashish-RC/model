package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ra1/model-gateway/internal/keypool"
	"github.com/ra1/model-gateway/internal/server"
	"github.com/ra1/model-gateway/internal/store"
	"github.com/ra1/model-gateway/test/mockprovider"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	scimToken := os.Getenv("GATEWAY_SCIM_TOKEN")
	srv := server.NewServer(server.Config{
		SCIMToken: scimToken,
	})

	// Provision default test virtual key if none exists
	rawKey := os.Getenv("GATEWAY_API_KEY")
	if rawKey == "" {
		rawKey = "sk-gw-default-key-1234567890abcdef"
	}
	keyHash := store.HashKey(rawKey)

	_ = srv.KeyStore.SaveKey(&store.VirtualKey{
		ID:        "vk-admin-default",
		KeyHash:   keyHash,
		KeyPrefix: "default",
		Name:      "Admin Default Key",
		OrgID:     "org-default",
		IsActive:  true,
	})
	srv.Wallet.SetBalance("org-default", 1000000000000) // $1,000,000 balance

	// If OPENAI_API_KEY is not provided in environment, default to mock provider
	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey == "" {
		mock := mockprovider.NewServer()
		if os.Getenv("BENCHMARK_MODE") == "1" {
			mock.DisableRecording = true
			mock.DefaultTTFT = 0
		}
		log.Printf("No OPENAI_API_KEY provided; started local mock provider at %s", mock.URL)
		srv.KeyPool.AddKey(&keypool.Key{
			ID:       "mock-openai-key",
			Provider: "openai",
			APIKey:   "sk-mock-key",
			BaseURL:  mock.URL + "/v1",
			PoolType: keypool.PoolShared,
			LimitRPM: 0, // unlimited for mock/benchmark
		})
	} else {
		srv.KeyPool.AddKey(&keypool.Key{
			ID:       "prod-openai-key",
			Provider: "openai",
			APIKey:   openaiKey,
			PoolType: keypool.PoolShared,
			LimitRPM: 500,
		})
	}

	httpServer := &http.Server{
		Addr:         ":" + port,
		Handler:      srv,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	go func() {
		log.Printf("Gateway listening on :%s (Default Virtual Key: %s)", port, rawKey)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down gateway server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("server forced shutdown: %v", err)
	}
	fmt.Println("Gateway stopped gracefully")
}
