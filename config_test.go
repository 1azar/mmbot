package mmbot

import (
	"testing"
	"time"
)

func TestConfigNormalized(t *testing.T) {
	t.Parallel()

	config, err := (Config{
		ServerURL: "https://mattermost.example.com/",
		Token:     "token",
		ChannelIDs: []string{
			"channel", "", "channel",
		},
	}).normalized()
	if err != nil {
		t.Fatal(err)
	}

	if config.ServerURL != "https://mattermost.example.com" {
		t.Fatalf("unexpected URL: %q", config.ServerURL)
	}
	if config.Prefix != "!" {
		t.Fatalf("unexpected prefix: %q", config.Prefix)
	}
	if config.ReconnectMin != time.Second || config.ReconnectMax != 30*time.Second {
		t.Fatalf("unexpected reconnect defaults: %s..%s", config.ReconnectMin, config.ReconnectMax)
	}
	if config.HandlerConcurrency != 16 || config.HandlerQueueSize != 128 {
		t.Fatalf("unexpected worker defaults: %d/%d", config.HandlerConcurrency, config.HandlerQueueSize)
	}
	if config.QueuePolicy != QueuePolicyBlock || config.ShutdownTimeout != 30*time.Second {
		t.Fatalf("unexpected lifecycle defaults: %v/%v", config.QueuePolicy, config.ShutdownTimeout)
	}
	if len(config.ChannelIDs) != 1 || config.ChannelIDs[0] != "channel" {
		t.Fatalf("unexpected channel IDs: %#v", config.ChannelIDs)
	}
}

func TestConfigValidation(t *testing.T) {
	t.Parallel()

	tests := []Config{
		{Token: "token"},
		{ServerURL: "localhost", Token: "token"},
		{ServerURL: "https://example.com"},
		{ServerURL: "https://example.com", Token: "token", Prefix: " !"},
		{ServerURL: "https://example.com", Token: "token", ReconnectMin: 2 * time.Second, ReconnectMax: time.Second},
		{ServerURL: "https://example.com", Token: "token", HandlerConcurrency: -1},
		{ServerURL: "https://example.com", Token: "token", QueuePolicy: 99},
		{ServerURL: "https://example.com", Token: "token", ShutdownTimeout: -1},
	}

	for _, config := range tests {
		if _, err := config.normalized(); err == nil {
			t.Fatalf("expected validation error for %#v", config)
		}
	}
}
