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
