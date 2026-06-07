package mmbot

import (
	"fmt"

	"github.com/mattermost/mattermost/server/public/model"
)

type webSocket interface {
	Events() <-chan *model.WebSocketEvent
	Responses() <-chan *model.WebSocketResponse
	PingTimeouts() <-chan bool
	Listen()
	Close()
	Err() error
}

type sdkWebSocket struct {
	client *model.WebSocketClient
}

func (s *sdkWebSocket) Events() <-chan *model.WebSocketEvent       { return s.client.EventChannel }
func (s *sdkWebSocket) Responses() <-chan *model.WebSocketResponse { return s.client.ResponseChannel }
func (s *sdkWebSocket) PingTimeouts() <-chan bool                  { return s.client.PingTimeoutChannel }
func (s *sdkWebSocket) Listen()                                    { s.client.Listen() }
func (s *sdkWebSocket) Close()                                     { s.client.Close() }
func (s *sdkWebSocket) Err() error {
	if s.client.ListenError == nil {
		return nil
	}
	return s.client.ListenError
}

func connectWebSocket(serverURL, token string) (webSocket, error) {
	client, err := model.NewWebSocketClient4(serverURL, token)
	if err != nil {
		return nil, fmt.Errorf("mmbot: connect websocket: %w", err)
	}
	return &sdkWebSocket{client: client}, nil
}
