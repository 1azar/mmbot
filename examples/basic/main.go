package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"

	"github.com/1azar/mmbot"
)

func main() {
	bot, err := mmbot.New(mmbot.Config{
		ServerURL: os.Getenv("MM_URL"),
		Token:     os.Getenv("MM_TOKEN"),
	}, mmbot.WithLogger(slog.Default()))
	if err != nil {
		log.Fatal(err)
	}

	if err := bot.Use(mmbot.Logger(slog.Default())); err != nil {
		log.Fatal(err)
	}
	if err := bot.HandleCommand("ping", func(ctx *mmbot.Context) error {
		return ctx.Reply("pong")
	}, mmbot.Description("Check that the bot is running")); err != nil {
		log.Fatal(err)
	}
	if err := bot.HandleCommand("help", func(ctx *mmbot.Context) error {
		return ctx.Reply(bot.Help())
	}, mmbot.Description("List available commands")); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := bot.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
