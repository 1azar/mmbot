package mmbot

import (
	"context"
	"errors"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
)

type fakeAPI struct {
	created   []*model.Post
	me        *model.User
	users     map[string]*model.User
	getUser   int
	getMeErr  error
	createErr error
	userErr   error
}

func (f *fakeAPI) CreatePost(_ context.Context, post *model.Post) (*model.Post, *model.Response, error) {
	if f.createErr != nil {
		return nil, nil, f.createErr
	}
	f.created = append(f.created, post)
	return &model.Post{
		Id:        "created",
		ChannelId: post.ChannelId,
		RootId:    post.RootId,
		Message:   post.Message,
	}, nil, nil
}

func (f *fakeAPI) GetMe(context.Context, string) (*model.User, *model.Response, error) {
	return f.me, nil, f.getMeErr
}

func (f *fakeAPI) GetUser(_ context.Context, id, _ string) (*model.User, *model.Response, error) {
	f.getUser++
	return f.users[id], nil, f.userErr
}

func TestContextHelpersAndClientErrors(t *testing.T) {
	t.Parallel()

	want := errors.New("api failed")
	api := &fakeAPI{createErr: want, userErr: want}
	client := &sdkClient{raw: api}
	ctx := NewContext(nil, client, ContextInput{
		Message: Message{ID: "post", ChannelID: "channel"},
		Command: "ping",
		Args:    []string{"one"},
		RawArgs: "one",
	})
	if ctx.Context() == nil || ctx.Client() != client || ctx.Message().ID != "post" {
		t.Fatal("unexpected context accessors")
	}
	if err := ctx.Replyf("hello %s", "world"); !errors.Is(err, want) {
		t.Fatalf("expected reply error, got %v", err)
	}
	if err := ctx.Post("hello"); !errors.Is(err, want) {
		t.Fatalf("expected post error, got %v", err)
	}
	if _, err := client.User(context.Background(), "user"); !errors.Is(err, want) {
		t.Fatalf("expected user error, got %v", err)
	}
	if client.Mattermost() != nil {
		t.Fatal("unexpected SDK client")
	}
	if postFromModel(nil) != nil || userFromModel(nil) != nil {
		t.Fatal("nil models must stay nil")
	}
}

func TestContextReplyUsesExistingThread(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{}
	ctx := NewContext(context.Background(), &sdkClient{raw: api}, ContextInput{
		Message: Message{
			ID:        "reply",
			RootID:    "root",
			ChannelID: "channel",
		},
	})

	if err := ctx.Reply("hello"); err != nil {
		t.Fatal(err)
	}
	if len(api.created) != 1 || api.created[0].RootId != "root" {
		t.Fatalf("unexpected post: %#v", api.created)
	}
}

func TestContextReplyStartsThreadAtIncomingPost(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{}
	ctx := NewContext(context.Background(), &sdkClient{raw: api}, ContextInput{
		Message: Message{
			ID:        "root",
			ChannelID: "channel",
		},
	})
	if err := ctx.Reply("hello"); err != nil {
		t.Fatal(err)
	}
	if api.created[0].RootId != "root" {
		t.Fatalf("unexpected root ID: %q", api.created[0].RootId)
	}
}
