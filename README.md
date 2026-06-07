# mmbot

`mmbot` is a Go package for building Mattermost bots with command routing,
middleware, guards, ordered concurrent execution, reconnect handling, and
graceful shutdown.

```go
bot, err := mmbot.New(mmbot.Config{
    ServerURL: os.Getenv("MM_URL"),
    Token:     os.Getenv("MM_TOKEN"),
})
if err != nil {
    log.Fatal(err)
}

err = bot.HandleCommand("deploy", func(ctx *mmbot.Context) error {
    service, environment := ctx.Args()[0], ctx.Args()[1]
    return ctx.Replyf("Deploying `%s` to `%s`", service, environment)
},
    mmbot.Description("Deploy a service"),
    mmbot.Usage(`!deploy "service name" <environment>`),
    mmbot.Aliases("ship"),
    mmbot.CommandGuards(mmbot.AllowUsernames("alice")),
)
if err != nil {
    log.Fatal(err)
}

ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
defer stop()
if err := bot.Run(ctx); err != nil {
    log.Fatal(err)
}
```

## Routing

Commands take priority over mentions and ordinary messages. Arguments support
single and double quotes, backslash escapes, empty quoted values, and adjacent
fragments:

```text
!deploy "service api" production
!echo one\ value "" prefix"suffix"
```

Use `bot.Use` for global middleware and `CommandMiddleware` or
`RouteMiddleware` for one route. Guards return `nil` to allow execution and an
error to reject it.

## Concurrency and shutdown

Messages in one channel execute in FIFO order; different channels execute
concurrently up to `HandlerConcurrency`. `QueuePolicyBlock` applies
backpressure, while `QueuePolicyDropNewest` calls the configured overflow
handler. Cancelling `Run` cancels active handler contexts and waits up to
`ShutdownTimeout`.

`Bot` is single-use. Register all routes before calling `Run`.

## Testing handlers

```go
harness := mmbottest.New()
ctx := harness.Context().
    WithMessage(mmbot.Message{ID: "post", ChannelID: "channel"}).
    WithCommand("ping").
    Build()

err := handler(ctx)
posts := harness.Client().Posts()
```

The fake client can also configure users and injected API errors.

## Mattermost SDK

Common operations use package-owned `Message`, `Post`, and `User` types. Use
`ctx.Client().Mattermost()` when an operation is only available in the official
SDK.

## Examples

- [`examples/basic`](examples/basic) is the smallest runnable bot.
- [`examples/comprehensive`](examples/comprehensive) demonstrates nearly all
  runtime APIs with detailed comments, plus handler tests using `mmbottest`.

Run the comprehensive example with:

```sh
MM_URL=https://mattermost.example.com \
MM_TOKEN=your-token \
go run ./examples/comprehensive
```

Optional environment variables configure the example without editing code:

- `MM_PREFIX`
- `MM_CHANNEL_IDS`, `MM_TEAM_IDS`
- `MM_ADMIN_USER_IDS`, `MM_ADMIN_USERNAMES`
- `MM_COMMAND_CHANNEL_IDS`, `MM_COMMAND_TEAM_IDS`
- `MM_QUEUE_POLICY=block|drop`

```sh
go test -race ./...
go vet ./...
go build ./...
```
