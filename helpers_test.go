package mmbot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

func TestSmallLifecycleHelpers(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected sleep error: %v", err)
	}
	if contextOf(nil) == nil {
		t.Fatal("nil callback context must produce background context")
	}
	if minDuration(time.Second, 2*time.Second) != time.Second ||
		minDuration(2*time.Second, time.Second) != time.Second {
		t.Fatal("unexpected minimum")
	}
	parseErr := &CommandParseError{Offset: 12, Reason: "bad input"}
	if parseErr.Error() != "mmbot: invalid command syntax at byte 12: bad input" {
		t.Fatalf("unexpected error: %s", parseErr)
	}
}

func TestSDKWebSocketAdapter(t *testing.T) {
	t.Parallel()

	raw := &model.WebSocketClient{
		EventChannel:       make(chan *model.WebSocketEvent),
		ResponseChannel:    make(chan *model.WebSocketResponse),
		PingTimeoutChannel: make(chan bool),
	}
	socket := &sdkWebSocket{client: raw}
	if socket.Events() == nil || socket.Responses() == nil || socket.PingTimeouts() == nil {
		t.Fatal("adapter did not expose SDK channels")
	}
	if socket.Err() != nil {
		t.Fatal("unexpected adapter error")
	}
	raw.ListenError = model.NewAppError("test", "test", nil, "", 500)
	if socket.Err() == nil {
		t.Fatal("expected adapter error")
	}
}

func TestWebSocketURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		serverURL string
		want      string
	}{
		{serverURL: "http://mattermost.example.com", want: "ws://mattermost.example.com"},
		{serverURL: "https://mattermost.example.com", want: "wss://mattermost.example.com"},
		{serverURL: "https://example.com/mattermost", want: "wss://example.com/mattermost"},
	}

	for _, test := range tests {
		got := webSocketURL(test.serverURL)
		if got != test.want {
			t.Fatalf("webSocketURL(%q) = %q, want %q", test.serverURL, got, test.want)
		}
	}
}
