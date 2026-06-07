// Package mmbottest provides fakes and builders for testing mmbot handlers.
package mmbottest

import (
	"context"
	"strconv"
	"sync"

	"github.com/1azar/mmbot"
	"github.com/mattermost/mattermost/server/public/model"
)

// Harness owns a fake Client and records posts created by handlers.
type Harness struct {
	client *Client
}

// New creates an empty handler test harness.
func New() *Harness {
	return &Harness{
		client: &Client{
			users:      make(map[string]*mmbot.User),
			userErrors: make(map[string]error),
		},
	}
}

// Client returns the fake mmbot client.
func (h *Harness) Client() *Client { return h.client }

// Context starts a builder attached to this harness.
func (h *Harness) Context() *ContextBuilder {
	return &ContextBuilder{
		harness: h,
		ctx:     context.Background(),
	}
}

// Client is an in-memory implementation of mmbot.Client.
type Client struct {
	mu sync.Mutex

	posts       []mmbot.Post
	users       map[string]*mmbot.User
	userErrors  map[string]error
	createError error
}

// CreatePost records a post or returns the configured error.
func (c *Client) CreatePost(_ context.Context, post mmbot.Post) (*mmbot.Post, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.createError != nil {
		return nil, c.createError
	}
	post.ID = "post-" + strconv.Itoa(len(c.posts)+1)
	c.posts = append(c.posts, post)
	result := post
	return &result, nil
}

// User returns a configured user or error. An unknown ID returns nil, nil.
func (c *Client) User(_ context.Context, userID string) (*mmbot.User, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.userErrors[userID]; err != nil {
		return nil, err
	}
	user := c.users[userID]
	if user == nil {
		return nil, nil
	}
	result := *user
	return &result, nil
}

// Mattermost returns nil because the harness does not use the real SDK.
func (c *Client) Mattermost() *model.Client4 { return nil }

// Posts returns a copy of all posts created through the fake client.
func (c *Client) Posts() []mmbot.Post {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]mmbot.Post(nil), c.posts...)
}

// SetUser configures a user returned by User.
func (c *Client) SetUser(user mmbot.User) {
	c.mu.Lock()
	defer c.mu.Unlock()
	copy := user
	c.users[user.ID] = &copy
	delete(c.userErrors, user.ID)
}

// SetUserError configures an error returned for a user ID.
func (c *Client) SetUserError(userID string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.userErrors[userID] = err
}

// SetCreatePostError configures the error returned by CreatePost.
func (c *Client) SetCreatePostError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.createError = err
}

// ContextBuilder constructs a synthetic mmbot.Context.
type ContextBuilder struct {
	harness *Harness
	ctx     context.Context
	input   mmbot.ContextInput
}

// WithContext sets the standard context.
func (b *ContextBuilder) WithContext(ctx context.Context) *ContextBuilder {
	b.ctx = ctx
	return b
}

// WithMessage sets the incoming message.
func (b *ContextBuilder) WithMessage(message mmbot.Message) *ContextBuilder {
	b.input.Message = message
	return b
}

// WithCommand sets the canonical command name.
func (b *ContextBuilder) WithCommand(command string) *ContextBuilder {
	b.input.Command = command
	return b
}

// WithArgs sets parsed and raw command arguments.
func (b *ContextBuilder) WithArgs(raw string, args ...string) *ContextBuilder {
	b.input.RawArgs = raw
	b.input.Args = append([]string(nil), args...)
	return b
}

// Build creates the context.
func (b *ContextBuilder) Build() *mmbot.Context {
	return mmbot.NewContext(b.ctx, b.harness.client, b.input)
}
