package mmbot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

type runtimeClient interface {
	Client
	currentUser(context.Context) (*User, error)
}

type cachedUsername struct {
	value     string
	expiresAt time.Time
}

// Bot routes Mattermost messages to registered handlers.
type Bot struct {
	config Config
	logger *slog.Logger

	mu     sync.RWMutex
	hasRun bool
	frozen bool

	commands map[string]*commandRoute
	mention  *route
	message  *route

	middleware     []Middleware
	unknownCommand Handler
	onError        ErrorHandler
	onUnauthorized UnauthorizedHandler
	onOverflow     OverflowHandler
	onParseError   CommandParseErrorHandler

	client Client
	me     *User

	usersMu   sync.RWMutex
	usernames map[string]cachedUsername

	newClient    func(string, string) runtimeClient
	connect      func(string, string) (webSocket, error)
	sleep        func(context.Context, time.Duration) error
	randDuration func(time.Duration) time.Duration
	now          func() time.Time
}

// New validates config and creates a single-use bot.
func New(config Config, options ...Option) (*Bot, error) {
	config, err := config.normalized()
	if err != nil {
		return nil, err
	}

	bot := &Bot{
		config:    config,
		logger:    slog.Default(),
		commands:  make(map[string]*commandRoute),
		usernames: make(map[string]cachedUsername),
		newClient: func(serverURL, token string) runtimeClient {
			api := model.NewAPIv4Client(serverURL)
			api.SetToken(token)
			return newClient(api)
		},
		connect: connectWebSocket,
		sleep:   sleepContext,
		randDuration: func(max time.Duration) time.Duration {
			if max <= 0 {
				return 0
			}
			return time.Duration(rand.Int64N(int64(max)))
		},
		now: time.Now,
	}
	bot.onError = func(ctx *Context, err error) {
		bot.logger.ErrorContext(contextOf(ctx), "mmbot handler error", "error", err)
	}
	bot.onUnauthorized = func(ctx *Context, err error) error {
		bot.logger.DebugContext(contextOf(ctx), "mmbot route rejected", "error", err)
		return nil
	}
	bot.onOverflow = func(ctx *Context, err error) {
		bot.logger.WarnContext(contextOf(ctx), "mmbot handler queue full", "error", err)
	}
	bot.onParseError = func(ctx *Context, err *CommandParseError) error {
		bot.logger.DebugContext(contextOf(ctx), "mmbot command parse failed", "error", err)
		return nil
	}

	for _, option := range options {
		if option != nil {
			option.applyBot(bot)
		}
	}
	return bot, nil
}

// Run connects to Mattermost and blocks until ctx is cancelled.
// A Bot is single-use, including when the first Run returns an error.
func (b *Bot) Run(ctx context.Context) error {
	b.mu.Lock()
	if b.hasRun {
		b.mu.Unlock()
		return ErrAlreadyRun
	}
	b.hasRun = true
	b.frozen = true
	b.mu.Unlock()

	client := b.newClient(b.config.ServerURL, b.config.Token)
	me, err := client.currentUser(ctx)
	if err != nil {
		return err
	}
	b.client = client
	b.me = me

	scheduler := newScheduler(ctx, b.config, b.runJob)
	runErr := b.connectionLoop(ctx, scheduler)
	shutdownErr := scheduler.Shutdown(b.config.ShutdownTimeout)
	if shutdownErr != nil {
		return shutdownErr
	}
	return runErr
}

func (b *Bot) connectionLoop(ctx context.Context, scheduler *scheduler) error {
	backoff := b.config.ReconnectMin
	for {
		if ctx.Err() != nil {
			return nil
		}

		socket, err := b.connect(b.config.WebSocketURL, b.config.Token)
		if err != nil {
			b.logger.WarnContext(ctx, "mmbot websocket connection failed", "error", err, "retry_in", backoff)
			if err := b.waitReconnect(ctx, backoff); err != nil {
				return nil
			}
			backoff = minDuration(backoff*2, b.config.ReconnectMax)
			continue
		}

		backoff = b.config.ReconnectMin
		socket.Listen()
		err = b.consumeSocket(ctx, socket, scheduler)
		socket.Close()
		if ctx.Err() != nil {
			return nil
		}
		b.logger.WarnContext(ctx, "mmbot websocket disconnected", "error", err, "retry_in", backoff)
		if err := b.waitReconnect(ctx, backoff); err != nil {
			return nil
		}
		backoff = minDuration(backoff*2, b.config.ReconnectMax)
	}
}

func (b *Bot) consumeSocket(ctx context.Context, socket webSocket, scheduler *scheduler) error {
	connectionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	status := make(chan error, 1)
	go monitorSocket(connectionCtx, cancel, socket, status)

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-status:
			return err
		case event, ok := <-socket.Events():
			if !ok {
				return socketError(socket)
			}
			if event == nil || event.EventType() != model.WebsocketEventPosted {
				continue
			}
			job, ok := b.routeEvent(scheduler.Context(), event)
			if !ok {
				continue
			}
			err := scheduler.Submit(connectionCtx, job.ctx.Message().ChannelID, job)
			switch {
			case err == nil:
			case errors.Is(err, ErrHandlerQueueFull):
				b.onOverflow(job.ctx, err)
			case isSubmitCancellation(err):
				select {
				case statusErr := <-status:
					return statusErr
				case <-ctx.Done():
					return nil
				}
			default:
				return err
			}
		}
	}
}

func monitorSocket(ctx context.Context, cancel context.CancelFunc, socket webSocket, status chan<- error) {
	send := func(err error) {
		cancel()
		select {
		case status <- err:
		default:
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-socket.Responses():
			if !ok {
				send(socketError(socket))
				return
			}
		case _, ok := <-socket.PingTimeouts():
			if !ok {
				send(socketError(socket))
			} else {
				send(errors.New("mmbot: websocket ping timeout"))
			}
			return
		}
	}
}

func (b *Bot) routeEvent(ctx context.Context, event *model.WebSocketEvent) (handlerJob, bool) {
	message, err := b.messageFromEvent(ctx, event)
	if err != nil {
		b.onError(nil, err)
		return handlerJob{}, false
	}
	if message.UserID == b.me.ID || !b.allowed(message) {
		return handlerJob{}, false
	}

	parsed, parseErr := parseCommand(b.config.Prefix, message.Text)
	mentioned, mentionParseErr, startsWithMention := parseMentionCommand(b.me.Username, message.Text)
	if startsWithMention {
		if command := b.commands[mentioned.name]; mentioned.ok && command != nil && command.config.mentionRequired {
			parsed = mentioned
			parseErr = mentionParseErr
		}
	}
	handlerCtx := NewContext(ctx, b.client, ContextInput{
		Message: message,
		Command: parsed.name,
		Args:    parsed.args,
		RawArgs: parsed.rawArgs,
	})
	if parseErr != nil {
		if command := b.commands[parsed.name]; command != nil && command.config.mentionRequired && !startsWithMention {
			return handlerJob{}, false
		}
		if err := b.onParseError(handlerCtx, parseErr); err != nil {
			b.onError(handlerCtx, err)
		}
		return handlerJob{}, false
	}

	if parsed.ok {
		command := b.commands[parsed.name]
		if command != nil {
			if command.config.mentionRequired && !startsWithMention {
				return handlerJob{}, false
			}
			handlerCtx.command = command.name
			return handlerJob{handler: b.prepare(command.handler, command.config.guards, command.config.middleware), ctx: handlerCtx}, true
		}
		if b.unknownCommand != nil {
			return handlerJob{handler: b.prepare(b.unknownCommand, nil, nil), ctx: handlerCtx}, true
		}
		return handlerJob{}, false
	}

	if b.mention != nil && containsMention(message.Text, b.me.Username) {
		return handlerJob{handler: b.prepare(b.mention.handler, b.mention.guards, b.mention.middleware), ctx: handlerCtx}, true
	}
	if b.message != nil {
		return handlerJob{handler: b.prepare(b.message.handler, b.message.guards, b.message.middleware), ctx: handlerCtx}, true
	}
	return handlerJob{}, false
}

func (b *Bot) prepare(handler Handler, guards []Guard, routeMiddleware []Middleware) Handler {
	routed := applyMiddleware(handler, routeMiddleware)
	guarded := func(ctx *Context) error {
		if err := AllGuards(guards...)(ctx); err != nil {
			if callbackErr := b.onUnauthorized(ctx, err); callbackErr != nil {
				b.onError(ctx, callbackErr)
			}
			return nil
		}
		return routed(ctx)
	}
	return applyMiddleware(guarded, b.middleware)
}

func (b *Bot) runJob(job handlerJob) {
	defer func() {
		if recovered := recover(); recovered != nil {
			b.onError(job.ctx, fmt.Errorf("mmbot: handler panic: %v\n%s", recovered, debug.Stack()))
		}
	}()
	if err := job.handler(job.ctx); err != nil {
		b.onError(job.ctx, err)
	}
}

func (b *Bot) messageFromEvent(ctx context.Context, event *model.WebSocketEvent) (Message, error) {
	raw, ok := event.GetData()["post"].(string)
	if !ok {
		return Message{}, errors.New("mmbot: posted event has no post")
	}
	var post model.Post
	if err := json.Unmarshal([]byte(raw), &post); err != nil {
		return Message{}, fmt.Errorf("mmbot: decode posted event: %w", err)
	}

	username, _ := event.GetData()["sender_name"].(string)
	if username == "" {
		username = b.cachedUsername(post.UserId)
		if username == "" {
			user, err := b.client.User(ctx, post.UserId)
			if err != nil {
				b.logger.DebugContext(ctx, "mmbot username lookup failed", "user_id", post.UserId, "error", err)
			} else if user != nil {
				username = user.Username
				b.cacheUsername(post.UserId, username)
			}
		}
	}

	teamID := ""
	if broadcast := event.GetBroadcast(); broadcast != nil {
		teamID = broadcast.TeamId
	}
	return Message{
		ID:        post.Id,
		RootID:    post.RootId,
		UserID:    post.UserId,
		Username:  username,
		ChannelID: post.ChannelId,
		TeamID:    teamID,
		Text:      post.Message,
		CreateAt:  time.UnixMilli(post.CreateAt),
	}, nil
}

func (b *Bot) cachedUsername(userID string) string {
	b.usersMu.RLock()
	entry, ok := b.usernames[userID]
	b.usersMu.RUnlock()
	if !ok || !b.now().Before(entry.expiresAt) {
		if ok {
			b.usersMu.Lock()
			delete(b.usernames, userID)
			b.usersMu.Unlock()
		}
		return ""
	}
	return entry.value
}

func (b *Bot) cacheUsername(userID, username string) {
	if userID == "" || username == "" {
		return
	}
	b.usersMu.Lock()
	b.usernames[userID] = cachedUsername{
		value:     username,
		expiresAt: b.now().Add(5 * time.Minute),
	}
	b.usersMu.Unlock()
}

func (b *Bot) allowed(message Message) bool {
	return (len(b.config.ChannelIDs) == 0 || containsString(b.config.ChannelIDs, message.ChannelID)) &&
		(len(b.config.TeamIDs) == 0 || containsString(b.config.TeamIDs, message.TeamID))
}

func containsMention(text, username string) bool {
	if username == "" {
		return false
	}
	needle := "@" + username
	for start := 0; ; {
		index := strings.Index(text[start:], needle)
		if index < 0 {
			return false
		}
		index += start
		end := index + len(needle)
		beforeOK := index == 0 || !isUsernameRune(rune(text[index-1]))
		afterOK := end == len(text) || !isUsernameRune(rune(text[end]))
		if beforeOK && afterOK {
			return true
		}
		start = end
	}
}

func isUsernameRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_'
}

func (b *Bot) waitReconnect(ctx context.Context, base time.Duration) error {
	return b.sleep(ctx, base+b.randDuration(base/5))
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func socketError(socket webSocket) error {
	if err := socket.Err(); err != nil {
		return fmt.Errorf("mmbot: websocket closed: %w", err)
	}
	return errors.New("mmbot: websocket closed")
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

func contextOf(ctx *Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx.Context()
}
