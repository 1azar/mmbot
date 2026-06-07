package mmbot_test

import (
	"context"
	"testing"

	"github.com/1azar/mmbot"
	"github.com/1azar/mmbot/mmbottest"
)

func TestPublicHandlerAPI(t *testing.T) {
	t.Parallel()

	bot, err := mmbot.New(mmbot.Config{
		ServerURL: "https://mattermost.example.com",
		Token:     "token",
	}, mmbot.WithUnknownCommandHandler(func(*mmbot.Context) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	if err := bot.HandleCommand("deploy", func(ctx *mmbot.Context) error {
		return ctx.Replyf("deploying %s", ctx.Args()[0])
	},
		mmbot.Description("Deploy a service"),
		mmbot.Aliases("ship"),
		mmbot.CommandGuards(mmbot.AllowUsernames("alice")),
	); err != nil {
		t.Fatal(err)
	}

	harness := mmbottest.New()
	ctx := mmbot.NewContext(context.Background(), harness.Client(), mmbot.ContextInput{
		Message: mmbot.Message{ID: "post", ChannelID: "channel"},
		Command: "deploy",
		Args:    []string{"api"},
		RawArgs: "api",
	})
	if ctx.Client() == nil || ctx.Command() != "deploy" {
		t.Fatalf("unexpected public context: %#v", ctx)
	}
}
