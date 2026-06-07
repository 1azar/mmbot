package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/1azar/mmbot"
	"github.com/mattermost/mattermost/server/public/model"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Config contains connection settings, optional global allowlists, reconnect
	// behavior, and limits for ordered concurrent handler execution.
	config := mmbot.Config{
		ServerURL:    os.Getenv("MM_URL"),
		WebSocketURL: os.Getenv("MM_WEBSOCKET_URL"),
		Token:        os.Getenv("MM_TOKEN"),
		Prefix:       envOrDefault("MM_PREFIX", "!"),

		ChannelIDs: splitEnv("MM_CHANNEL_IDS"),
		TeamIDs:    splitEnv("MM_TEAM_IDS"),

		ReconnectMin: 500 * time.Millisecond,
		ReconnectMax: 30 * time.Second,

		HandlerConcurrency: 4,
		HandlerQueueSize:   64,
		QueuePolicy:        queuePolicy(os.Getenv("MM_QUEUE_POLICY")),
		ShutdownTimeout:    10 * time.Second,
	}

	// Bot options configure cross-cutting callbacks. The package is silent by
	// default, so applications decide which errors should be shown to users.
	bot, err := mmbot.New(
		config,
		mmbot.WithLogger(logger),
		mmbot.WithErrorHandler(func(ctx *mmbot.Context, err error) {
			logger.ErrorContext(callbackContext(ctx), "bot error", "error", err)
		}),
		mmbot.WithUnauthorizedHandler(func(ctx *mmbot.Context, err error) error {
			if !errors.Is(err, mmbot.ErrUnauthorized) {
				return err
			}
			logger.InfoContext(ctx.Context(), "route rejected",
				"user", ctx.Message().Username,
				"error", err,
			)
			return ctx.Reply("You are not allowed to use this command.")
		}),
		mmbot.WithOverflowHandler(func(ctx *mmbot.Context, err error) {
			if !errors.Is(err, mmbot.ErrHandlerQueueFull) {
				logger.ErrorContext(ctx.Context(), "unexpected queue error", "error", err)
				return
			}
			logger.WarnContext(ctx.Context(), "message dropped",
				"channel_id", ctx.Message().ChannelID,
				"error", err,
			)
		}),
		mmbot.WithCommandParseErrorHandler(func(ctx *mmbot.Context, err *mmbot.CommandParseError) error {
			if !errors.Is(err, mmbot.ErrInvalidCommandSyntax) {
				return err
			}
			return ctx.Replyf(
				"Invalid command syntax at byte %d: %s. Check your quotes and escapes.",
				err.Offset,
				err.Reason,
			)
		}),
		mmbot.WithUnknownCommandHandler(func(ctx *mmbot.Context) error {
			return ctx.Replyf("Unknown command `%s`. Try `%shelp`.", ctx.Command(), config.Prefix)
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Global middleware wraps every routed handler, including rejected guards.
	if err := bot.Use(
		mmbot.Logger(logger),
		requestIDMiddleware(logger),
	); err != nil {
		log.Fatal(err)
	}

	// Guards are ordinary functions and can be composed. Empty env lists make
	// these examples permissive, while populated lists enable real allowlists.
	adminGuard := optionalAnyGuard(
		mmbot.AllowUserIDs(splitEnv("MM_ADMIN_USER_IDS")...),
		mmbot.AllowUsernames(splitEnv("MM_ADMIN_USERNAMES")...),
		splitEnv("MM_ADMIN_USER_IDS"),
		splitEnv("MM_ADMIN_USERNAMES"),
	)
	locationGuard := optionalAllGuard(
		mmbot.AllowChannelIDs(splitEnv("MM_COMMAND_CHANNEL_IDS")...),
		mmbot.AllowTeamIDs(splitEnv("MM_COMMAND_TEAM_IDS")...),
		splitEnv("MM_COMMAND_CHANNEL_IDS"),
		splitEnv("MM_COMMAND_TEAM_IDS"),
	)

	// Simple commands demonstrate Reply, metadata used by Help, and aliases.
	must(bot.HandleCommand("ping", pingHandler,
		mmbot.Description("Check that the bot is alive"),
	))
	must(bot.HandleCommand("echo", echoHandler,
		mmbot.Description("Echo parsed and raw arguments"),
		mmbot.Usage(`!echo "quoted value" escaped\ value`),
		mmbot.Aliases("say"),
	))

	// This command combines command-specific middleware and composed guards.
	must(bot.HandleCommand("deploy", deployHandler,
		mmbot.Description("Simulate a service deployment"),
		mmbot.Usage(`!deploy "service name" <environment>`),
		mmbot.Aliases("ship"),
		mmbot.CommandMiddleware(auditMiddleware(logger)),
		mmbot.CommandGuards(mmbot.AllGuards(adminGuard, locationGuard)),
	))

	// These handlers demonstrate the package-owned Client and model types.
	must(bot.HandleCommand("whoami", whoAmIHandler,
		mmbot.Description("Read the sender through Client.User"),
	))
	must(bot.HandleCommand("announce", announceHandler,
		mmbot.Description("Create a new root post in the current channel"),
		mmbot.Usage(`!announce "message"`),
		mmbot.CommandGuards(adminGuard),
	))

	// Hidden commands remain routable but are excluded from generated help.
	// The SDK escape hatch is useful for operations not wrapped by mmbot.
	must(bot.HandleCommand("sdk", sdkHandler,
		mmbot.Hidden(),
		mmbot.CommandGuards(adminGuard),
	))

	// Commands and Help expose deterministic command metadata.
	must(bot.HandleCommand("help", func(ctx *mmbot.Context) error {
		return ctx.Reply(bot.Help())
	}, mmbot.Description("Show generated command help")))
	must(bot.HandleCommand("commands", func(ctx *mmbot.Context) error {
		return commandListHandler(ctx, bot.Commands())
	}, mmbot.Description("Show command metadata")))

	// Mention and fallback message routes can have their own guards and middleware.
	must(bot.HandleMention(mentionHandler(config.Prefix),
		mmbot.RouteMiddleware(auditMiddleware(logger)),
		mmbot.RouteGuards(locationGuard),
	))
	must(bot.HandleMessage(messageHandler(logger),
		mmbot.RouteMiddleware(auditMiddleware(logger)),
	))

	// Run is single-use and blocks until the signal context is cancelled.
	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := bot.Run(runCtx); err != nil {
		if errors.Is(err, mmbot.ErrShutdownTimeout) {
			logger.Error("handlers did not stop before the shutdown deadline")
		}
		log.Fatal(err)
	}
}

func pingHandler(ctx *mmbot.Context) error {
	return ctx.Reply("pong")
}

func echoHandler(ctx *mmbot.Context) error {
	return ctx.Replyf("parsed=%q\nraw=%q", ctx.Args(), ctx.RawArgs())
}

func deployHandler(ctx *mmbot.Context) error {
	args := ctx.Args()
	if len(args) != 2 {
		return ctx.Reply(`Usage: !deploy "service name" <environment>`)
	}

	// This example intentionally simulates deployment instead of running an
	// external command or changing infrastructure.
	return ctx.Replyf("Simulating deployment of `%s` to `%s`.", args[0], args[1])
}

func whoAmIHandler(ctx *mmbot.Context) error {
	message := ctx.Message()
	user, err := ctx.Client().User(ctx.Context(), message.UserID)
	if err != nil {
		return fmt.Errorf("get sender: %w", err)
	}
	if user == nil {
		return ctx.Replyf("No user found for ID `%s`.", message.UserID)
	}
	return ctx.Replyf(
		"You are @%s (%s %s), bot=%t.",
		user.Username,
		user.FirstName,
		user.LastName,
		user.IsBot,
	)
}

func announceHandler(ctx *mmbot.Context) error {
	if ctx.RawArgs() == "" {
		return ctx.Reply(`Usage: !announce "message"`)
	}

	// Post creates a root post, unlike Reply which posts into the current thread.
	return ctx.Post(strings.Join(ctx.Args(), " "))
}

func sdkHandler(ctx *mmbot.Context) error {
	sdk := ctx.Client().Mattermost()
	if sdk == nil {
		return errors.New("Mattermost SDK client is unavailable")
	}

	status, _, err := sdk.GetPingWithOptions(ctx.Context(), model.SystemPingOptions{})
	if err != nil {
		return fmt.Errorf("Mattermost ping: %w", err)
	}

	// CreatePost shows the package-owned Post model. Most handlers can use the
	// shorter Context.Reply or Context.Post helpers instead.
	_, err = ctx.Client().CreatePost(ctx.Context(), mmbot.Post{
		ChannelID: ctx.Message().ChannelID,
		RootID:    replyRoot(ctx.Message()),
		Message:   fmt.Sprintf("Mattermost SDK ping result: `%v`", status),
	})
	return err
}

func mentionHandler(prefix string) mmbot.Handler {
	return func(ctx *mmbot.Context) error {
		return ctx.Replyf("Hello @%s. Try `%shelp`.", ctx.Message().Username, prefix)
	}
}

func messageHandler(logger *slog.Logger) mmbot.Handler {
	return func(ctx *mmbot.Context) error {
		message := ctx.Message()
		logger.DebugContext(ctx.Context(), "ordinary message ignored",
			"post_id", message.ID,
			"root_id", message.RootID,
			"user_id", message.UserID,
			"username", message.Username,
			"channel_id", message.ChannelID,
			"team_id", message.TeamID,
			"text", message.Text,
			"created_at", message.CreateAt,
		)
		return nil
	}
}

func commandListHandler(ctx *mmbot.Context, commands []mmbot.CommandInfo) error {
	var builder strings.Builder
	for _, command := range commands {
		fmt.Fprintf(
			&builder,
			"name=%s aliases=%v hidden=%t usage=%q description=%q\n",
			command.Name,
			command.Aliases,
			command.Hidden,
			command.Usage,
			command.Description,
		)
	}
	return ctx.Reply(builder.String())
}

func requestIDMiddleware(logger *slog.Logger) mmbot.Middleware {
	return func(next mmbot.Handler) mmbot.Handler {
		return func(ctx *mmbot.Context) error {
			message := ctx.Message()
			logger.DebugContext(ctx.Context(), "message routed",
				"post_id", message.ID,
				"command", ctx.Command(),
			)
			return next(ctx)
		}
	}
}

func auditMiddleware(logger *slog.Logger) mmbot.Middleware {
	return func(next mmbot.Handler) mmbot.Handler {
		return func(ctx *mmbot.Context) error {
			logger.InfoContext(ctx.Context(), "route audit",
				"user", ctx.Message().Username,
				"command", ctx.Command(),
			)
			return next(ctx)
		}
	}
}

func optionalAnyGuard(userIDGuard, usernameGuard mmbot.Guard, userIDs, usernames []string) mmbot.Guard {
	if len(userIDs) == 0 && len(usernames) == 0 {
		return func(*mmbot.Context) error { return nil }
	}
	return mmbot.AnyGuard(userIDGuard, usernameGuard)
}

func optionalAllGuard(channelGuard, teamGuard mmbot.Guard, channelIDs, teamIDs []string) mmbot.Guard {
	var guards []mmbot.Guard
	if len(channelIDs) > 0 {
		guards = append(guards, channelGuard)
	}
	if len(teamIDs) > 0 {
		guards = append(guards, teamGuard)
	}
	return mmbot.AllGuards(guards...)
}

func replyRoot(message mmbot.Message) string {
	if message.RootID != "" {
		return message.RootID
	}
	return message.ID
}

func queuePolicy(value string) mmbot.QueuePolicy {
	if strings.EqualFold(strings.TrimSpace(value), "drop") {
		return mmbot.QueuePolicyDropNewest
	}
	return mmbot.QueuePolicyBlock
}

func splitEnv(name string) []string {
	var values []string
	for _, value := range strings.Split(os.Getenv(name), ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func callbackContext(ctx *mmbot.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx.Context()
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
