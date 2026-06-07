package mmbot

import (
	"context"
	"fmt"
)

// Context contains an incoming message and helpers available to handlers.
type Context struct {
	ctx     context.Context
	client  Client
	message Message
	command string
	args    []string
	rawArgs string
}

// ContextInput contains synthetic handler input used by adapters and tests.
type ContextInput struct {
	Message Message
	Command string
	Args    []string
	RawArgs string
}

// NewContext creates a handler context from package-owned values.
func NewContext(ctx context.Context, client Client, input ContextInput) *Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Context{
		ctx:     ctx,
		client:  client,
		message: input.Message,
		command: input.Command,
		args:    append([]string(nil), input.Args...),
		rawArgs: input.RawArgs,
	}
}

// Context returns the cancellation context for this handler invocation.
func (c *Context) Context() context.Context { return c.ctx }

// Client returns the client used by this handler.
func (c *Context) Client() Client { return c.client }

// Message returns the incoming message.
func (c *Context) Message() Message { return c.message }

// Command returns the canonical command name, including when an alias matched.
func (c *Context) Command() string { return c.command }

// RawArgs returns the unparsed argument substring.
func (c *Context) RawArgs() string { return c.rawArgs }

// Args returns a copy of parsed command arguments.
func (c *Context) Args() []string {
	return append([]string(nil), c.args...)
}

// Reply posts in the incoming message's existing thread, or starts a thread
// rooted at the incoming message when it is a root post.
func (c *Context) Reply(text string) error {
	rootID := c.message.RootID
	if rootID == "" {
		rootID = c.message.ID
	}
	_, err := c.client.CreatePost(c.ctx, Post{
		ChannelID: c.message.ChannelID,
		RootID:    rootID,
		Message:   text,
	})
	return err
}

// Replyf formats and posts a threaded reply.
func (c *Context) Replyf(format string, args ...any) error {
	return c.Reply(fmt.Sprintf(format, args...))
}

// Post creates a root post in the incoming message's channel.
func (c *Context) Post(text string) error {
	_, err := c.client.CreatePost(c.ctx, Post{
		ChannelID: c.message.ChannelID,
		Message:   text,
	})
	return err
}
