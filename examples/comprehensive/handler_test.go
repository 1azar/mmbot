package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/1azar/mmbot"
	"github.com/1azar/mmbot/mmbottest"
)

func TestDeployHandlerWithHarness(t *testing.T) {
	t.Parallel()

	// The harness creates a fake Client, so this test never contacts Mattermost.
	harness := mmbottest.New()
	ctx := harness.Context().
		WithContext(context.Background()).
		WithMessage(mmbot.Message{
			ID:        "incoming-post",
			UserID:    "user-1",
			Username:  "alice",
			ChannelID: "channel-1",
			TeamID:    "team-1",
		}).
		WithCommand("deploy").
		WithArgs(`"service api" production`, "service api", "production").
		Build()

	if err := deployHandler(ctx); err != nil {
		t.Fatal(err)
	}

	posts := harness.Client().Posts()
	if len(posts) != 1 {
		t.Fatalf("posts = %#v", posts)
	}
	if posts[0].RootID != "incoming-post" {
		t.Fatalf("reply root = %q", posts[0].RootID)
	}
	if !strings.Contains(posts[0].Message, "service api") {
		t.Fatalf("reply = %q", posts[0].Message)
	}
}

func TestWhoAmIHandlerWithFakeUser(t *testing.T) {
	t.Parallel()

	harness := mmbottest.New()
	harness.Client().SetUser(mmbot.User{
		ID:        "user-1",
		Username:  "alice",
		FirstName: "Alice",
		LastName:  "Example",
	})
	ctx := harness.Context().
		WithMessage(mmbot.Message{
			ID:        "post-1",
			UserID:    "user-1",
			ChannelID: "channel-1",
		}).
		Build()

	if err := whoAmIHandler(ctx); err != nil {
		t.Fatal(err)
	}
	if got := harness.Client().Posts()[0].Message; !strings.Contains(got, "@alice") {
		t.Fatalf("reply = %q", got)
	}
}

func TestInjectedClientErrors(t *testing.T) {
	t.Parallel()

	harness := mmbottest.New()
	want := errors.New("Mattermost unavailable")

	// SetUserError demonstrates failures from Client.User.
	harness.Client().SetUserError("user-1", want)
	userCtx := harness.Context().
		WithMessage(mmbot.Message{
			UserID:    "user-1",
			ChannelID: "channel-1",
		}).
		Build()
	if err := whoAmIHandler(userCtx); !errors.Is(err, want) {
		t.Fatalf("expected user error, got %v", err)
	}

	// SetCreatePostError demonstrates failures from Reply, Replyf, and Post.
	harness.Client().SetCreatePostError(want)
	postCtx := harness.Context().
		WithMessage(mmbot.Message{
			ID:        "post-1",
			ChannelID: "channel-1",
		}).
		WithArgs("maintenance", "maintenance").
		Build()
	if err := announceHandler(postCtx); !errors.Is(err, want) {
		t.Fatalf("expected create-post error, got %v", err)
	}
}

func TestNewContextWithoutBuilder(t *testing.T) {
	t.Parallel()

	harness := mmbottest.New()

	// NewContext is useful for custom adapters. Most handler tests can use the
	// more readable mmbottest ContextBuilder shown above.
	ctx := mmbot.NewContext(context.Background(), harness.Client(), mmbot.ContextInput{
		Message: mmbot.Message{
			ID:        "post-1",
			ChannelID: "channel-1",
		},
		Command: "echo",
		Args:    []string{"quoted value"},
		RawArgs: `"quoted value"`,
	})

	if err := echoHandler(ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.Command() != "echo" || ctx.RawArgs() != `"quoted value"` {
		t.Fatalf("unexpected context: command=%q raw=%q", ctx.Command(), ctx.RawArgs())
	}
}

func TestExampleConfigurationHelpers(t *testing.T) {
	t.Setenv("LIST", " one, two ,,three ")
	if got := splitEnv("LIST"); len(got) != 3 {
		t.Fatalf("splitEnv = %#v", got)
	}
	if queuePolicy("DROP") != mmbot.QueuePolicyDropNewest {
		t.Fatal("drop policy was not selected")
	}
	if queuePolicy("") != mmbot.QueuePolicyBlock {
		t.Fatal("block policy should be the default")
	}
}
