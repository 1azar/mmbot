package mmbot

import "time"

// Message is a stable representation of an incoming Mattermost post.
type Message struct {
	ID        string
	RootID    string
	UserID    string
	Username  string
	ChannelID string
	TeamID    string
	Text      string
	CreateAt  time.Time
}

// Post describes a post returned from or sent to Mattermost.
type Post struct {
	ID        string
	RootID    string
	UserID    string
	ChannelID string
	Message   string
	CreateAt  time.Time
}

// User contains the commonly needed Mattermost user fields.
type User struct {
	ID        string
	Username  string
	Email     string
	FirstName string
	LastName  string
	Nickname  string
	IsBot     bool
}

// CommandInfo describes a registered command.
type CommandInfo struct {
	Name            string
	Description     string
	Usage           string
	Aliases         []string
	Hidden          bool
	MentionRequired bool
}
