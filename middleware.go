package mmbot

import (
	"log/slog"
	"time"
)

// Handler processes one routed Mattermost message.
type Handler func(*Context) error

// Middleware wraps a Handler.
type Middleware func(Handler) Handler

// Guard returns nil to allow a route or an error to reject it.
type Guard func(*Context) error

// Logger records handler execution using structured logging.
func Logger(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next Handler) Handler {
		return func(ctx *Context) error {
			start := time.Now()
			err := next(ctx)
			logger.InfoContext(
				ctx.Context(),
				"mmbot handler completed",
				"command", ctx.Command(),
				"user_id", ctx.Message().UserID,
				"channel_id", ctx.Message().ChannelID,
				"duration", time.Since(start),
				"error", err,
			)
			return err
		}
	}
}

func applyMiddleware(handler Handler, middleware []Middleware) Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	return handler
}
