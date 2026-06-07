package mmbot

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

type fakeSocket struct {
	events    chan *model.WebSocketEvent
	responses chan *model.WebSocketResponse
	pings     chan bool
	closed    atomic.Bool
	err       error
}

func newFakeSocket() *fakeSocket {
	return &fakeSocket{
		events:    make(chan *model.WebSocketEvent, 8),
		responses: make(chan *model.WebSocketResponse, 1),
		pings:     make(chan bool, 1),
	}
}

func (f *fakeSocket) Events() <-chan *model.WebSocketEvent       { return f.events }
func (f *fakeSocket) Responses() <-chan *model.WebSocketResponse { return f.responses }
func (f *fakeSocket) PingTimeouts() <-chan bool                  { return f.pings }
func (f *fakeSocket) Listen()                                    {}
func (f *fakeSocket) Close()                                     { f.closed.Store(true) }
func (f *fakeSocket) Err() error                                 { return f.err }

func TestRoutePriorityAndAlias(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t)
	bot.client = &sdkClient{raw: &fakeAPI{users: map[string]*model.User{}}}
	bot.me = &User{ID: "bot", Username: "helper"}

	var called string
	if err := bot.HandleCommand("deploy", func(ctx *Context) error {
		called = ctx.Command() + ":" + ctx.RawArgs()
		return nil
	}, Aliases("ship")); err != nil {
		t.Fatal(err)
	}
	if err := bot.HandleMention(func(*Context) error {
		called = "mention"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := bot.HandleMessage(func(*Context) error {
		called = "message"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	job, ok := bot.routeEvent(context.Background(), postedEvent("!ship api", "user", "alice", "channel", "team"))
	if !ok {
		t.Fatal("expected route")
	}
	if err := job.handler(job.ctx); err != nil {
		t.Fatal(err)
	}
	if called != "deploy:api" {
		t.Fatalf("unexpected handler: %q", called)
	}
}

func TestMentionRequiredCommandRouting(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t)
	bot.client = &sdkClient{raw: &fakeAPI{users: map[string]*model.User{}}}
	bot.me = &User{ID: "bot", Username: "helper"}

	var calls []string
	if err := bot.HandleCommand("deploy", func(ctx *Context) error {
		calls = append(calls, ctx.Command()+":"+ctx.RawArgs()+":"+strings.Join(ctx.Args(), "|"))
		return nil
	},
		RequireMention(),
		Aliases("ship"),
		CommandMiddleware(func(next Handler) Handler {
			return func(ctx *Context) error {
				calls = append(calls, "middleware")
				return next(ctx)
			}
		}),
	); err != nil {
		t.Fatal(err)
	}
	if err := bot.HandleCommand("ping", func(ctx *Context) error {
		calls = append(calls, ctx.Command())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := bot.HandleMention(func(*Context) error {
		calls = append(calls, "mention")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := bot.HandleMessage(func(*Context) error {
		calls = append(calls, "message")
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	run := func(text string) bool {
		t.Helper()
		job, ok := bot.routeEvent(context.Background(), postedEvent(text, "user", "alice", "channel", "team"))
		if ok {
			if err := job.handler(job.ctx); err != nil {
				t.Fatal(err)
			}
		}
		return ok
	}

	if !run(`@helper ship "service api" prod\ eu`) {
		t.Fatal("mention command was not routed")
	}
	if want := []string{"middleware", `deploy:"service api" prod\ eu:service api|prod eu`}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	calls = nil

	if run("!deploy prod") || len(calls) != 0 {
		t.Fatalf("prefixed mention command was not ignored: %#v", calls)
	}
	if !run("hello @helper deploy prod") || !reflect.DeepEqual(calls, []string{"mention"}) {
		t.Fatalf("non-leading mention did not use mention route: %#v", calls)
	}
	calls = nil

	if !run("!ping") || !reflect.DeepEqual(calls, []string{"ping"}) {
		t.Fatalf("ordinary command failed: %#v", calls)
	}
	calls = nil

	if !run("@helper unknown text") || !reflect.DeepEqual(calls, []string{"mention"}) {
		t.Fatalf("unknown mention text did not use mention route: %#v", calls)
	}
	if !run("@helper !deploy prod") || !reflect.DeepEqual(calls, []string{"mention", "mention"}) {
		t.Fatalf("unsupported mention syntax did not use mention route: %#v", calls)
	}
}

func TestMentionRequiredCommandGuardAndParseError(t *testing.T) {
	t.Parallel()

	var parseErrors atomic.Int32
	var unauthorized atomic.Int32
	var handled atomic.Int32
	bot, err := New(Config{
		ServerURL: "https://mattermost.example.com",
		Token:     "token",
	}, WithCommandParseErrorHandler(func(_ *Context, parseErr *CommandParseError) error {
		if errors.Is(parseErr, ErrInvalidCommandSyntax) {
			parseErrors.Add(1)
		}
		return nil
	}), WithUnauthorizedHandler(func(*Context, error) error {
		unauthorized.Add(1)
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	bot.client = &sdkClient{raw: &fakeAPI{users: map[string]*model.User{}}}
	bot.me = &User{ID: "bot", Username: "helper"}
	if err := bot.HandleCommand("deploy", func(*Context) error {
		handled.Add(1)
		return nil
	}, RequireMention(), CommandGuards(AllowUserIDs("allowed"))); err != nil {
		t.Fatal(err)
	}

	if _, ok := bot.routeEvent(context.Background(), postedEvent(
		`@helper deploy "unterminated`, "user", "alice", "channel", "team",
	)); ok {
		t.Fatal("malformed mention command was routed")
	}
	if parseErrors.Load() != 1 {
		t.Fatalf("parse errors = %d, want 1", parseErrors.Load())
	}

	if _, ok := bot.routeEvent(context.Background(), postedEvent(
		`!deploy "unterminated`, "user", "alice", "channel", "team",
	)); ok {
		t.Fatal("malformed prefixed mention command was routed")
	}
	if parseErrors.Load() != 1 {
		t.Fatalf("prefixed call reported a parse error: %d", parseErrors.Load())
	}

	job, ok := bot.routeEvent(context.Background(), postedEvent(
		"@helper deploy prod", "user", "alice", "channel", "team",
	))
	if !ok {
		t.Fatal("guarded mention command was not routed")
	}
	if err := job.handler(job.ctx); err != nil {
		t.Fatal(err)
	}
	if unauthorized.Load() != 1 || handled.Load() != 0 {
		t.Fatalf("unauthorized=%d handled=%d", unauthorized.Load(), handled.Load())
	}
}

func TestGuardAndMiddlewareOrder(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t)
	var mu sync.Mutex
	var calls []string
	record := func(value string) {
		mu.Lock()
		calls = append(calls, value)
		mu.Unlock()
	}

	if err := bot.Use(func(next Handler) Handler {
		return func(ctx *Context) error {
			record("global-before")
			err := next(ctx)
			record("global-after")
			return err
		}
	}); err != nil {
		t.Fatal(err)
	}
	handler := bot.prepare(func(*Context) error {
		record("handler")
		return nil
	}, nil, []Middleware{func(next Handler) Handler {
		return func(ctx *Context) error {
			record("route-before")
			err := next(ctx)
			record("route-after")
			return err
		}
	}})

	if err := handler(&Context{ctx: context.Background()}); err != nil {
		t.Fatal(err)
	}
	want := []string{"global-before", "route-before", "handler", "route-after", "global-after"}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("unexpected order: %#v", calls)
		}
	}
}

func TestUnauthorizedHandlerSkipsCommand(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t)
	var handled atomic.Bool
	var unauthorized atomic.Bool
	bot.onUnauthorized = func(_ *Context, err error) error {
		if errors.Is(err, ErrUnauthorized) {
			unauthorized.Store(true)
		}
		return nil
	}
	handler := bot.prepare(func(*Context) error {
		handled.Store(true)
		return nil
	}, []Guard{AllowUserIDs("allowed")}, nil)

	if err := handler(&Context{
		ctx:     context.Background(),
		message: Message{UserID: "other"},
	}); err != nil {
		t.Fatal(err)
	}
	if handled.Load() || !unauthorized.Load() {
		t.Fatalf("handled=%v unauthorized=%v", handled.Load(), unauthorized.Load())
	}
}

func TestRouteFiltersAndUnknownCommand(t *testing.T) {
	t.Parallel()

	bot, err := New(Config{
		ServerURL: "https://mattermost.example.com",
		Token:     "token",
		ChannelIDs: []string{
			"allowed-channel",
		},
	}, WithUnknownCommandHandler(func(*Context) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	bot.client = &sdkClient{raw: &fakeAPI{users: map[string]*model.User{}}}
	bot.me = &User{ID: "bot", Username: "helper"}

	if _, ok := bot.routeEvent(context.Background(), postedEvent(
		"!missing", "user", "alice", "other-channel", "team",
	)); ok {
		t.Fatal("message from filtered channel was routed")
	}
	if _, ok := bot.routeEvent(context.Background(), postedEvent(
		"!missing", "user", "alice", "allowed-channel", "team",
	)); !ok {
		t.Fatal("unknown command handler was not routed")
	}
}

func TestCommandParseErrorCallback(t *testing.T) {
	t.Parallel()

	var parsed atomic.Bool
	var handled atomic.Bool
	bot, err := New(Config{
		ServerURL: "https://mattermost.example.com",
		Token:     "token",
	}, WithCommandParseErrorHandler(func(_ *Context, parseErr *CommandParseError) error {
		if errors.Is(parseErr, ErrInvalidCommandSyntax) {
			parsed.Store(true)
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	bot.client = &sdkClient{raw: &fakeAPI{users: map[string]*model.User{}}}
	bot.me = &User{ID: "bot", Username: "helper"}
	if err := bot.HandleCommand("deploy", func(*Context) error {
		handled.Store(true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if _, ok := bot.routeEvent(context.Background(), postedEvent(
		`!deploy "unterminated`, "user", "alice", "channel", "team",
	)); ok {
		t.Fatal("malformed command was routed")
	}
	if !parsed.Load() || handled.Load() {
		t.Fatalf("parsed=%v handled=%v", parsed.Load(), handled.Load())
	}
}

func TestUsernameLookupIsCached(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t)
	api := &fakeAPI{users: map[string]*model.User{
		"user": {Id: "user", Username: "alice"},
	}}
	bot.client = &sdkClient{raw: api}
	bot.me = &User{ID: "bot", Username: "helper"}
	if err := bot.HandleMessage(func(*Context) error { return nil }); err != nil {
		t.Fatal(err)
	}

	event := postedEvent("hello", "user", "", "channel", "team")
	if _, ok := bot.routeEvent(context.Background(), event); !ok {
		t.Fatal("expected first route")
	}
	if _, ok := bot.routeEvent(context.Background(), event); !ok {
		t.Fatal("expected second route")
	}
	if api.getUser != 1 {
		t.Fatalf("expected one user lookup, got %d", api.getUser)
	}
}

func TestUsernameCacheExpires(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t)
	now := time.Unix(100, 0)
	bot.now = func() time.Time { return now }
	api := &fakeAPI{users: map[string]*model.User{
		"user": {Id: "user", Username: "alice"},
	}}
	bot.client = &sdkClient{raw: api}
	bot.me = &User{ID: "bot", Username: "helper"}
	if err := bot.HandleMessage(func(*Context) error { return nil }); err != nil {
		t.Fatal(err)
	}

	event := postedEvent("hello", "user", "", "channel", "team")
	if _, ok := bot.routeEvent(context.Background(), event); !ok {
		t.Fatal("expected route")
	}
	now = now.Add(6 * time.Minute)
	if _, ok := bot.routeEvent(context.Background(), event); !ok {
		t.Fatal("expected route after expiry")
	}
	if api.getUser != 2 {
		t.Fatalf("expected two lookups, got %d", api.getUser)
	}
}

func TestListenReportsFullHandlerQueue(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t)
	bot.client = &sdkClient{raw: &fakeAPI{users: map[string]*model.User{}}}
	bot.me = &User{ID: "bot", Username: "helper"}
	if err := bot.HandleMessage(func(*Context) error { return nil }); err != nil {
		t.Fatal(err)
	}

	reported := make(chan error, 1)
	bot.onOverflow = func(_ *Context, err error) { reported <- err }
	socket := newFakeSocket()
	bot.config.QueuePolicy = QueuePolicyDropNewest
	bot.config.HandlerQueueSize = 1
	scheduler := newScheduler(context.Background(), bot.config, func(handlerJob) {})
	defer scheduler.Shutdown(time.Second)
	scheduler.slots <- struct{}{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- bot.consumeSocket(ctx, socket, scheduler) }()
	socket.events <- postedEvent("hello", "user", "alice", "channel", "team")

	select {
	case err := <-reported:
		if !errors.Is(err, ErrHandlerQueueFull) {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queue overflow was not reported")
	}
	cancel()
	<-done
}

func TestBlockedSubmitStillObservesPingTimeout(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t)
	bot.config.HandlerQueueSize = 1
	bot.config.QueuePolicy = QueuePolicyBlock
	bot.client = &sdkClient{raw: &fakeAPI{users: map[string]*model.User{}}}
	bot.me = &User{ID: "bot", Username: "helper"}
	if err := bot.HandleMessage(func(*Context) error { return nil }); err != nil {
		t.Fatal(err)
	}

	scheduler := newScheduler(context.Background(), bot.config, func(handlerJob) {})
	scheduler.slots <- struct{}{}
	socket := newFakeSocket()
	done := make(chan error, 1)
	go func() { done <- bot.consumeSocket(context.Background(), socket, scheduler) }()
	socket.events <- postedEvent("hello", "user", "alice", "channel", "team")
	socket.pings <- true

	select {
	case err := <-done:
		if err == nil || err.Error() != "mmbot: websocket ping timeout" {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ping timeout did not unblock submit")
	}
	<-scheduler.slots
	if err := scheduler.Shutdown(time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestRunReconnectsAfterSocketClose(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t)
	api := &fakeAPI{me: &model.User{Id: "bot", Username: "helper"}}
	first := newFakeSocket()
	first.err = errors.New("connection lost")
	close(first.events)
	second := newFakeSocket()

	var connections atomic.Int32
	connectedTwice := make(chan struct{})
	bot.newClient = func(string, string) runtimeClient { return &sdkClient{raw: api} }
	bot.connect = func(string, string) (webSocket, error) {
		if connections.Add(1) == 1 {
			return first, nil
		}
		close(connectedTwice)
		return second, nil
	}
	bot.sleep = func(context.Context, time.Duration) error { return nil }
	bot.randDuration = func(time.Duration) time.Duration { return 0 }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- bot.Run(ctx) }()

	select {
	case <-connectedTwice:
		cancel()
	case <-time.After(time.Second):
		cancel()
		t.Fatal("bot did not reconnect")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if connections.Load() != 2 {
		t.Fatalf("unexpected connection count: %d", connections.Load())
	}
}

func TestRunUsesServerAndWebSocketURLs(t *testing.T) {
	t.Parallel()

	bot, err := New(Config{
		ServerURL:    "https://mattermost.example.com",
		WebSocketURL: "wss://socket.example.com/mattermost",
		Token:        "token",
	})
	if err != nil {
		t.Fatal(err)
	}

	api := &fakeAPI{me: &model.User{Id: "bot", Username: "helper"}}
	socket := newFakeSocket()
	var restURL, socketURL string
	connected := make(chan struct{})
	bot.newClient = func(serverURL, _ string) runtimeClient {
		restURL = serverURL
		return &sdkClient{raw: api}
	}
	bot.connect = func(serverURL, _ string) (webSocket, error) {
		socketURL = serverURL
		close(connected)
		return socket, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- bot.Run(ctx) }()
	select {
	case <-connected:
		cancel()
	case <-time.After(time.Second):
		cancel()
		t.Fatal("bot did not connect")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if restURL != "https://mattermost.example.com" {
		t.Fatalf("REST URL = %q", restURL)
	}
	if socketURL != "wss://socket.example.com/mattermost" {
		t.Fatalf("WebSocket URL = %q", socketURL)
	}
}

func TestWorkerRecoversPanics(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t)
	reported := make(chan error, 1)
	bot.onError = func(_ *Context, err error) { reported <- err }

	bot.runJob(handlerJob{
		ctx: &Context{ctx: context.Background()},
		handler: func(*Context) error {
			panic("boom")
		},
	})

	select {
	case err := <-reported:
		if err == nil {
			t.Fatal("expected panic error")
		}
	default:
		t.Fatal("expected error callback")
	}
}

func TestRunGracefulShutdownAndRouteFreeze(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t)
	api := &fakeAPI{me: &model.User{Id: "bot", Username: "helper"}}
	socket := newFakeSocket()
	bot.newClient = func(string, string) runtimeClient { return &sdkClient{raw: api} }
	bot.connect = func(string, string) (webSocket, error) { return socket, nil }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- bot.Run(ctx) }()

	deadline := time.Now().Add(time.Second)
	for {
		bot.mu.RLock()
		running := bot.hasRun
		bot.mu.RUnlock()
		if running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("bot did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if err := bot.HandleMessage(func(*Context) error { return nil }); !errors.Is(err, ErrRoutesFrozen) {
		t.Fatalf("expected frozen routes, got %v", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
	if !socket.closed.Load() {
		t.Fatal("websocket was not closed")
	}
}

func TestRunIsSingleUse(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t)
	api := &fakeAPI{getMeErr: errors.New("get me failed")}
	bot.newClient = func(string, string) runtimeClient { return &sdkClient{raw: api} }

	if err := bot.Run(context.Background()); err == nil {
		t.Fatal("expected initial GetMe error")
	}
	if err := bot.Run(context.Background()); !errors.Is(err, ErrAlreadyRun) {
		t.Fatalf("expected ErrAlreadyRun, got %v", err)
	}
}

func TestRunReturnsShutdownTimeout(t *testing.T) {
	t.Parallel()

	bot, err := New(Config{
		ServerURL:          "https://mattermost.example.com",
		Token:              "token",
		ShutdownTimeout:    10 * time.Millisecond,
		HandlerConcurrency: 1,
		HandlerQueueSize:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{me: &model.User{Id: "bot", Username: "helper"}}
	socket := newFakeSocket()
	bot.newClient = func(string, string) runtimeClient { return &sdkClient{raw: api} }
	bot.connect = func(string, string) (webSocket, error) { return socket, nil }

	started := make(chan struct{})
	release := make(chan struct{})
	if err := bot.HandleMessage(func(ctx *Context) error {
		close(started)
		if ctx.Context().Err() == nil {
			<-release
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- bot.Run(ctx) }()
	socket.events <- postedEvent("hello", "user", "alice", "channel", "team")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	cancel()
	if err := <-done; !errors.Is(err, ErrShutdownTimeout) {
		t.Fatalf("expected shutdown timeout, got %v", err)
	}
	close(release)
}

func postedEvent(text, userID, username, channelID, teamID string) *model.WebSocketEvent {
	raw, _ := json.Marshal(&model.Post{
		Id:        "post",
		UserId:    userID,
		ChannelId: channelID,
		Message:   text,
		CreateAt:  time.Now().UnixMilli(),
	})
	event := model.NewWebSocketEvent(model.WebsocketEventPosted, teamID, channelID, userID, nil, "")
	event.Add("post", string(raw))
	event.Add("sender_name", username)
	return event
}

func newTestBot(t *testing.T) *Bot {
	t.Helper()
	bot, err := New(Config{
		ServerURL: "https://mattermost.example.com",
		Token:     "token",
	})
	if err != nil {
		t.Fatal(err)
	}
	return bot
}
