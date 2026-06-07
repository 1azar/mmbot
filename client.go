package mmbot

import (
	"context"
	"fmt"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

type mattermostAPI interface {
	CreatePost(context.Context, *model.Post) (*model.Post, *model.Response, error)
	GetMe(context.Context, string) (*model.User, *model.Response, error)
	GetUser(context.Context, string, string) (*model.User, *model.Response, error)
}

// Client exposes common Mattermost operations using package-owned types.
type Client interface {
	CreatePost(context.Context, Post) (*Post, error)
	User(context.Context, string) (*User, error)
	Mattermost() *model.Client4
}

type sdkClient struct {
	api *model.Client4
	raw mattermostAPI
}

func newClient(api *model.Client4) *sdkClient {
	return &sdkClient{api: api, raw: api}
}

func (c *sdkClient) currentUser(ctx context.Context) (*User, error) {
	user, _, err := c.raw.GetMe(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("mmbot: get current user: %w", err)
	}
	return userFromModel(user), nil
}

// Mattermost returns the underlying SDK client for operations not wrapped by this package.
func (c *sdkClient) Mattermost() *model.Client4 {
	return c.api
}

// CreatePost creates a Mattermost post.
func (c *sdkClient) CreatePost(ctx context.Context, post Post) (*Post, error) {
	created, _, err := c.raw.CreatePost(ctx, &model.Post{
		ChannelId: post.ChannelID,
		RootId:    post.RootID,
		Message:   post.Message,
	})
	if err != nil {
		return nil, fmt.Errorf("mmbot: create post: %w", err)
	}
	return postFromModel(created), nil
}

// User retrieves a Mattermost user by ID.
func (c *sdkClient) User(ctx context.Context, userID string) (*User, error) {
	user, _, err := c.raw.GetUser(ctx, userID, "")
	if err != nil {
		return nil, fmt.Errorf("mmbot: get user %q: %w", userID, err)
	}
	return userFromModel(user), nil
}

func postFromModel(post *model.Post) *Post {
	if post == nil {
		return nil
	}
	return &Post{
		ID:        post.Id,
		RootID:    post.RootId,
		UserID:    post.UserId,
		ChannelID: post.ChannelId,
		Message:   post.Message,
		CreateAt:  time.UnixMilli(post.CreateAt),
	}
}

func userFromModel(user *model.User) *User {
	if user == nil {
		return nil
	}
	return &User{
		ID:        user.Id,
		Username:  user.Username,
		Email:     user.Email,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		Nickname:  user.Nickname,
		IsBot:     user.IsBot,
	}
}
