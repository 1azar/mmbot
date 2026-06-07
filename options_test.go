package mmbot

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
)

func TestOptionsAndLoggerMiddleware(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	want := errors.New("callback")
	bot, err := New(Config{
		ServerURL: "https://mattermost.example.com",
		Token:     "token",
	},
		WithLogger(logger),
		WithErrorHandler(func(*Context, error) {}),
		WithUnauthorizedHandler(func(*Context, error) error { return want }),
		WithOverflowHandler(func(*Context, error) {}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if bot.logger != logger {
		t.Fatal("logger option was not applied")
	}

	var called bool
	middleware := Logger(logger)
	handler := middleware(func(*Context) error {
		called = true
		return want
	})
	ctx := NewContext(context.Background(), nil, ContextInput{
		Message: Message{UserID: "user", ChannelID: "channel"},
		Command: "ping",
	})
	if err := handler(ctx); !errors.Is(err, want) || !called {
		t.Fatalf("called=%v err=%v", called, err)
	}
	if output.Len() == 0 {
		t.Fatal("logger middleware produced no output")
	}
}

func TestRouteAndCommandOptions(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t)
	middleware := func(next Handler) Handler { return next }
	guard := func(*Context) error { return nil }
	if err := bot.HandleCommand("command", func(*Context) error { return nil },
		CommandMiddleware(middleware),
		CommandGuards(guard),
	); err != nil {
		t.Fatal(err)
	}
	if err := bot.HandleMention(func(*Context) error { return nil },
		RouteMiddleware(middleware),
		RouteGuards(guard),
	); err != nil {
		t.Fatal(err)
	}
	if len(bot.commands["command"].config.middleware) != 1 ||
		len(bot.mention.middleware) != 1 ||
		len(bot.mention.guards) != 1 {
		t.Fatal("route options were not applied")
	}
}
