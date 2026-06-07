package mmbot

import "fmt"

// AllowUserIDs permits users whose Mattermost ID is listed.
func AllowUserIDs(ids ...string) Guard {
	allowed := stringSet(ids)
	return func(ctx *Context) error {
		if _, ok := allowed[ctx.Message().UserID]; ok {
			return nil
		}
		return ErrUnauthorized
	}
}

// AllowUsernames permits users whose username is listed.
func AllowUsernames(names ...string) Guard {
	allowed := stringSet(names)
	return func(ctx *Context) error {
		if _, ok := allowed[ctx.Message().Username]; ok {
			return nil
		}
		return ErrUnauthorized
	}
}

// AllowChannelIDs permits messages from listed channels.
func AllowChannelIDs(ids ...string) Guard {
	allowed := stringSet(ids)
	return func(ctx *Context) error {
		if _, ok := allowed[ctx.Message().ChannelID]; ok {
			return nil
		}
		return ErrUnauthorized
	}
}

// AllowTeamIDs permits messages from listed teams.
func AllowTeamIDs(ids ...string) Guard {
	allowed := stringSet(ids)
	return func(ctx *Context) error {
		if _, ok := allowed[ctx.Message().TeamID]; ok {
			return nil
		}
		return ErrUnauthorized
	}
}

// AllGuards permits a route only when every guard permits it.
func AllGuards(guards ...Guard) Guard {
	return func(ctx *Context) error {
		for i, guard := range guards {
			if guard == nil {
				continue
			}
			if err := guard(ctx); err != nil {
				return fmt.Errorf("guard %d: %w", i, err)
			}
		}
		return nil
	}
}

// AnyGuard permits a route when at least one guard permits it.
func AnyGuard(guards ...Guard) Guard {
	return func(ctx *Context) error {
		for _, guard := range guards {
			if guard != nil && guard(ctx) == nil {
				return nil
			}
		}
		return ErrUnauthorized
	}
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
