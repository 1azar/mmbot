package mmbottest_test

import (
	"errors"
	"testing"

	"github.com/1azar/mmbot"
	"github.com/1azar/mmbot/mmbottest"
)

func TestHarnessRecordsRepliesAndPosts(t *testing.T) {
	t.Parallel()

	harness := mmbottest.New()
	ctx := harness.Context().
		WithMessage(mmbot.Message{ID: "root", ChannelID: "channel"}).
		WithCommand("ping").
		WithArgs(`"hello world"`, "hello world").
		Build()

	if err := ctx.Reply("reply"); err != nil {
		t.Fatal(err)
	}
	if err := ctx.Post("root post"); err != nil {
		t.Fatal(err)
	}

	posts := harness.Client().Posts()
	if len(posts) != 2 {
		t.Fatalf("posts = %#v", posts)
	}
	if posts[0].RootID != "root" || posts[1].RootID != "" {
		t.Fatalf("unexpected roots: %#v", posts)
	}
}

func TestHarnessUsersAndErrors(t *testing.T) {
	t.Parallel()

	harness := mmbottest.New()
	harness.Client().SetUser(mmbot.User{ID: "user", Username: "alice"})
	user, err := harness.Client().User(t.Context(), "user")
	if err != nil || user.Username != "alice" {
		t.Fatalf("user=%#v err=%v", user, err)
	}

	want := errors.New("failed")
	harness.Client().SetCreatePostError(want)
	ctx := harness.Context().WithMessage(mmbot.Message{ID: "post", ChannelID: "channel"}).Build()
	if err := ctx.Reply("reply"); !errors.Is(err, want) {
		t.Fatalf("expected injected error, got %v", err)
	}
}
