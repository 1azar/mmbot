package mmbot

import (
	"errors"
	"reflect"
	"testing"
)

func TestParseCommand(t *testing.T) {
	t.Parallel()

	parsed, err := parseCommand("!", "  !Deploy  api   production ")
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.ok || parsed.name != "deploy" {
		t.Fatalf("unexpected command: %#v", parsed)
	}
	if parsed.rawArgs != "api   production" {
		t.Fatalf("unexpected raw args: %q", parsed.rawArgs)
	}
	if !reflect.DeepEqual(parsed.args, []string{"api", "production"}) {
		t.Fatalf("unexpected args: %#v", parsed.args)
	}
}

func TestParseCommandQuotesAndEscapes(t *testing.T) {
	t.Parallel()

	parsed, err := parseCommand("!", `!deploy "service api" 'prod eu' empty"" escaped\ value ""`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"service api", "prod eu", "empty", "escaped value", ""}
	if !reflect.DeepEqual(parsed.args, want) {
		t.Fatalf("args = %#v, want %#v", parsed.args, want)
	}
}

func TestParseCommandErrors(t *testing.T) {
	t.Parallel()

	_, err := parseCommand("!", `!deploy "missing`)
	if err == nil || !errors.Is(err, ErrInvalidCommandSyntax) || err.Offset != 8 {
		t.Fatalf("unexpected quote error: %#v", err)
	}
	_, err = parseCommand("!", `!deploy trailing\`)
	if err == nil || err.Reason != "trailing escape" {
		t.Fatalf("unexpected escape error: %#v", err)
	}
}

func TestCommandRegistrationAndHelp(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t)
	handler := func(*Context) error { return nil }

	if err := bot.HandleCommand("Deploy", handler,
		Description("Deploy a service"),
		Usage("!deploy <service>"),
		Aliases("ship"),
	); err != nil {
		t.Fatal(err)
	}
	if err := bot.HandleCommand("health", handler); err != nil {
		t.Fatal(err)
	}
	if err := bot.HandleCommand("restart", handler,
		RequireMention(),
		Description("Restart a service"),
		Usage("@bot restart <service>"),
	); err != nil {
		t.Fatal(err)
	}
	if err := bot.HandleCommand("secret", handler, Hidden()); err != nil {
		t.Fatal(err)
	}
	if err := bot.HandleCommand("ship", handler); err == nil {
		t.Fatal("expected duplicate alias error")
	}

	want := "- `!deploy <service>` - Deploy a service\n- `!health`\n- `@bot restart <service>` - Restart a service\n"
	if help := bot.Help(); help != want {
		t.Fatalf("unexpected help:\n%s", help)
	}

	bot.me = &User{Username: "helper"}
	want = "- `!deploy <service>` - Deploy a service\n- `!health`\n- `@helper restart <service>` - Restart a service\n"
	if help := bot.Help(); help != want {
		t.Fatalf("unexpected help with username:\n%s", help)
	}

	for _, command := range bot.Commands() {
		if command.Name == "restart" && !command.MentionRequired {
			t.Fatal("restart command does not expose MentionRequired")
		}
	}
}

func TestParseMentionCommand(t *testing.T) {
	t.Parallel()

	parsed, err, mentioned := parseMentionCommand(
		"helper",
		`  @helper deploy "service api" prod\ eu`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !mentioned || parsed.name != "deploy" || parsed.rawArgs != `"service api" prod\ eu` {
		t.Fatalf("unexpected command: %#v mentioned=%v", parsed, mentioned)
	}
	if want := []string{"service api", "prod eu"}; !reflect.DeepEqual(parsed.args, want) {
		t.Fatalf("args = %#v, want %#v", parsed.args, want)
	}

	if _, _, mentioned := parseMentionCommand("helper", "hello @helper deploy"); mentioned {
		t.Fatal("mention outside the beginning was parsed as a command")
	}
	if _, _, mentioned := parseMentionCommand("helper", "@helper-extra deploy"); mentioned {
		t.Fatal("partial username mention was parsed as a command")
	}
}

func TestGuards(t *testing.T) {
	t.Parallel()

	ctx := &Context{message: Message{
		UserID:    "user-1",
		Username:  "alice",
		ChannelID: "channel-1",
		TeamID:    "team-1",
	}}

	if err := AllGuards(
		AllowUserIDs("user-1"),
		AllowChannelIDs("channel-1"),
	)(ctx); err != nil {
		t.Fatal(err)
	}
	if err := AnyGuard(AllowUsernames("bob"), AllowTeamIDs("team-1"))(ctx); err != nil {
		t.Fatal(err)
	}
	if err := AllowUserIDs("other")(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
}

func TestContainsMention(t *testing.T) {
	t.Parallel()

	if !containsMention("hello @deploy.bot!", "deploy.bot") {
		t.Fatal("expected mention")
	}
	if containsMention("hello @deploy.bot-extra", "deploy.bot") {
		t.Fatal("unexpected partial mention")
	}
}
