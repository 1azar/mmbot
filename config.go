package mmbot

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	defaultPrefix             = "!"
	defaultReconnectMin       = time.Second
	defaultReconnectMax       = 30 * time.Second
	defaultHandlerConcurrency = 16
	defaultHandlerQueueSize   = 128
	defaultShutdownTimeout    = 30 * time.Second
)

// QueuePolicy controls behavior when the handler queue is full.
type QueuePolicy uint8

const (
	// QueuePolicyBlock waits for capacity or context cancellation.
	QueuePolicyBlock QueuePolicy = iota
	// QueuePolicyDropNewest rejects the newest message immediately.
	QueuePolicyDropNewest
)

// Config controls the Mattermost connection and message processing.
type Config struct {
	// ServerURL is the Mattermost base URL using the http or https scheme.
	ServerURL string
	// WebSocketURL overrides the WebSocket base URL derived from ServerURL.
	WebSocketURL string
	Token        string
	Prefix       string

	ChannelIDs []string
	TeamIDs    []string

	ReconnectMin time.Duration
	ReconnectMax time.Duration

	HandlerConcurrency int
	HandlerQueueSize   int
	QueuePolicy        QueuePolicy
	ShutdownTimeout    time.Duration
}

func (c Config) normalized() (Config, error) {
	c.ServerURL = strings.TrimRight(strings.TrimSpace(c.ServerURL), "/")
	c.WebSocketURL = strings.TrimRight(strings.TrimSpace(c.WebSocketURL), "/")
	c.Token = strings.TrimSpace(c.Token)

	if c.ServerURL == "" {
		return Config{}, errors.New("mmbot: ServerURL is required")
	}
	u, err := url.Parse(c.ServerURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return Config{}, fmt.Errorf("mmbot: ServerURL must be an absolute http(s) URL")
	}
	c.ServerURL = u.String()
	if c.WebSocketURL == "" {
		c.WebSocketURL = webSocketURL(c.ServerURL)
	} else {
		u, err = url.Parse(c.WebSocketURL)
		if err != nil || u.Host == "" || (u.Scheme != "ws" && u.Scheme != "wss") {
			return Config{}, fmt.Errorf("mmbot: WebSocketURL must be an absolute ws(s) URL")
		}
		c.WebSocketURL = u.String()
	}
	if c.Token == "" {
		return Config{}, errors.New("mmbot: Token is required")
	}

	if c.Prefix == "" {
		c.Prefix = defaultPrefix
	}
	if strings.TrimSpace(c.Prefix) != c.Prefix {
		return Config{}, errors.New("mmbot: Prefix must not start or end with whitespace")
	}

	if c.ReconnectMin == 0 {
		c.ReconnectMin = defaultReconnectMin
	}
	if c.ReconnectMax == 0 {
		c.ReconnectMax = defaultReconnectMax
	}
	if c.ReconnectMin < 0 || c.ReconnectMax < c.ReconnectMin {
		return Config{}, errors.New("mmbot: reconnect durations must be positive and ReconnectMax >= ReconnectMin")
	}

	if c.HandlerConcurrency == 0 {
		c.HandlerConcurrency = defaultHandlerConcurrency
	}
	if c.HandlerQueueSize == 0 {
		c.HandlerQueueSize = defaultHandlerQueueSize
	}
	if c.HandlerConcurrency < 1 || c.HandlerQueueSize < 1 {
		return Config{}, errors.New("mmbot: handler concurrency and queue size must be positive")
	}
	if c.QueuePolicy != QueuePolicyBlock && c.QueuePolicy != QueuePolicyDropNewest {
		return Config{}, errors.New("mmbot: unsupported queue policy")
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = defaultShutdownTimeout
	}
	if c.ShutdownTimeout < 0 {
		return Config{}, errors.New("mmbot: shutdown timeout must be positive")
	}

	c.ChannelIDs = compactStrings(c.ChannelIDs)
	c.TeamIDs = compactStrings(c.TeamIDs)
	return c, nil
}

func compactStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
