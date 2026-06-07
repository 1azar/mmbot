package mmbot

import "log/slog"

// ErrorHandler receives handler, callback, parsing, and internal processing errors.
type ErrorHandler func(*Context, error)

// UnauthorizedHandler handles a rejected guard. Its returned error is sent to ErrorHandler.
type UnauthorizedHandler func(*Context, error) error

// OverflowHandler handles a message rejected by QueuePolicyDropNewest.
type OverflowHandler func(*Context, error)

// CommandParseErrorHandler handles malformed command arguments.
type CommandParseErrorHandler func(*Context, *CommandParseError) error

// Option configures a Bot.
type Option interface {
	applyBot(*Bot)
}

type botOption func(*Bot)

func (option botOption) applyBot(bot *Bot) { option(bot) }

// WithLogger sets the logger used for lifecycle and default callback messages.
func WithLogger(logger *slog.Logger) Option {
	return botOption(func(bot *Bot) {
		if logger != nil {
			bot.logger = logger
		}
	})
}

// WithErrorHandler replaces the default structured error logger.
func WithErrorHandler(handler ErrorHandler) Option {
	return botOption(func(bot *Bot) {
		if handler != nil {
			bot.onError = handler
		}
	})
}

// WithUnauthorizedHandler handles guard rejections.
func WithUnauthorizedHandler(handler UnauthorizedHandler) Option {
	return botOption(func(bot *Bot) {
		if handler != nil {
			bot.onUnauthorized = handler
		}
	})
}

// WithUnknownCommandHandler handles prefixed commands that are not registered.
func WithUnknownCommandHandler(handler Handler) Option {
	return botOption(func(bot *Bot) {
		bot.unknownCommand = handler
	})
}

// WithOverflowHandler handles messages rejected by a full drop-newest queue.
func WithOverflowHandler(handler OverflowHandler) Option {
	return botOption(func(bot *Bot) {
		if handler != nil {
			bot.onOverflow = handler
		}
	})
}

// WithCommandParseErrorHandler handles malformed quoted command arguments.
func WithCommandParseErrorHandler(handler CommandParseErrorHandler) Option {
	return botOption(func(bot *Bot) {
		if handler != nil {
			bot.onParseError = handler
		}
	})
}

// CommandOption configures a command route.
type CommandOption interface {
	applyCommand(*commandConfig) error
}

type commandOption func(*commandConfig) error

func (option commandOption) applyCommand(config *commandConfig) error { return option(config) }

// Description sets help text for a command.
func Description(value string) CommandOption {
	return commandOption(func(config *commandConfig) error {
		config.description = value
		return nil
	})
}

// Usage sets the command usage rendered by Help.
func Usage(value string) CommandOption {
	return commandOption(func(config *commandConfig) error {
		config.usage = value
		return nil
	})
}

// Aliases registers additional names for a command.
func Aliases(values ...string) CommandOption {
	return commandOption(func(config *commandConfig) error {
		config.aliases = append(config.aliases, values...)
		return nil
	})
}

// Hidden excludes a command from Help while leaving it routable.
func Hidden() CommandOption {
	return commandOption(func(config *commandConfig) error {
		config.hidden = true
		return nil
	})
}

// RequireMention makes a command routable only when the message starts with
// the bot's @username instead of the configured command prefix.
func RequireMention() CommandOption {
	return commandOption(func(config *commandConfig) error {
		config.mentionRequired = true
		return nil
	})
}

// CommandMiddleware adds middleware to one command.
func CommandMiddleware(values ...Middleware) CommandOption {
	return commandOption(func(config *commandConfig) error {
		config.middleware = append(config.middleware, values...)
		return nil
	})
}

// CommandGuards adds authorization guards to one command.
func CommandGuards(values ...Guard) CommandOption {
	return commandOption(func(config *commandConfig) error {
		config.guards = append(config.guards, values...)
		return nil
	})
}

// RouteOption configures a mention or message route.
type RouteOption interface {
	applyRoute(*route)
}

type routeOption func(*route)

func (option routeOption) applyRoute(route *route) { option(route) }

// RouteMiddleware adds middleware to a mention or message route.
func RouteMiddleware(values ...Middleware) RouteOption {
	return routeOption(func(route *route) {
		route.middleware = append(route.middleware, values...)
	})
}

// RouteGuards adds authorization guards to a mention or message route.
func RouteGuards(values ...Guard) RouteOption {
	return routeOption(func(route *route) {
		route.guards = append(route.guards, values...)
	})
}
