package mmbot

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

type commandConfig struct {
	description string
	usage       string
	aliases     []string
	hidden      bool
	middleware  []Middleware
	guards      []Guard
}

type commandRoute struct {
	name    string
	handler Handler
	config  commandConfig
}

type route struct {
	handler    Handler
	middleware []Middleware
	guards     []Guard
}

// HandleCommand registers a named command and its options.
func (b *Bot) HandleCommand(name string, handler Handler, options ...CommandOption) error {
	if handler == nil {
		return errors.New("mmbot: command handler is nil")
	}

	name, err := normalizeCommandName(name)
	if err != nil {
		return err
	}

	config := commandConfig{}
	for _, option := range options {
		if option != nil {
			if err := option.applyCommand(&config); err != nil {
				return fmt.Errorf("mmbot: configure command %q: %w", name, err)
			}
		}
	}

	aliases := make([]string, 0, len(config.aliases))
	for _, alias := range config.aliases {
		alias, err = normalizeCommandName(alias)
		if err != nil {
			return fmt.Errorf("mmbot: command %q alias: %w", name, err)
		}
		if alias == name {
			continue
		}
		aliases = append(aliases, alias)
	}
	config.aliases = compactStrings(aliases)

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.frozen {
		return ErrRoutesFrozen
	}
	if _, exists := b.commands[name]; exists {
		return fmt.Errorf("mmbot: command %q is already registered", name)
	}
	for _, alias := range config.aliases {
		if _, exists := b.commands[alias]; exists {
			return fmt.Errorf("mmbot: command or alias %q is already registered", alias)
		}
	}

	command := &commandRoute{name: name, handler: handler, config: config}
	b.commands[name] = command
	for _, alias := range config.aliases {
		b.commands[alias] = command
	}
	return nil
}

// HandleMention registers the handler for messages that mention the bot.
func (b *Bot) HandleMention(handler Handler, options ...RouteOption) error {
	return b.setRoute(&b.mention, "mention", handler, options)
}

// HandleMessage registers the fallback handler for ordinary messages.
func (b *Bot) HandleMessage(handler Handler, options ...RouteOption) error {
	return b.setRoute(&b.message, "message", handler, options)
}

func (b *Bot) setRoute(target **route, name string, handler Handler, options []RouteOption) error {
	if handler == nil {
		return fmt.Errorf("mmbot: %s handler is nil", name)
	}
	value := &route{handler: handler}
	for _, option := range options {
		if option != nil {
			option.applyRoute(value)
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.frozen {
		return ErrRoutesFrozen
	}
	if *target != nil {
		return fmt.Errorf("mmbot: %s handler is already registered", name)
	}
	*target = value
	return nil
}

// Use appends global middleware in declaration order.
func (b *Bot) Use(middleware ...Middleware) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.frozen {
		return ErrRoutesFrozen
	}
	b.middleware = append(b.middleware, middleware...)
	return nil
}

// Commands returns registered canonical commands sorted by name.
func (b *Bot) Commands() []CommandInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make([]CommandInfo, 0, len(b.commands))
	seen := make(map[*commandRoute]struct{})
	for _, command := range b.commands {
		if _, ok := seen[command]; ok {
			continue
		}
		seen[command] = struct{}{}
		result = append(result, CommandInfo{
			Name:        command.name,
			Description: command.config.description,
			Usage:       command.config.usage,
			Aliases:     append([]string(nil), command.config.aliases...),
			Hidden:      command.config.hidden,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// Help renders a deterministic Markdown command list.
func (b *Bot) Help() string {
	var builder strings.Builder
	for _, command := range b.Commands() {
		if command.Hidden {
			continue
		}
		usage := command.Usage
		if usage == "" {
			usage = b.config.Prefix + command.Name
		}
		builder.WriteString("- `")
		builder.WriteString(usage)
		builder.WriteString("`")
		if command.Description != "" {
			builder.WriteString(" - ")
			builder.WriteString(command.Description)
		}
		builder.WriteByte('\n')
	}
	return builder.String()
}

type parsedCommand struct {
	name    string
	args    []string
	rawArgs string
	ok      bool
}

func parseCommand(prefix, text string) (parsedCommand, *CommandParseError) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, prefix) {
		return parsedCommand{}, nil
	}
	raw := strings.TrimSpace(strings.TrimPrefix(text, prefix))
	if raw == "" {
		return parsedCommand{}, nil
	}

	index := strings.IndexFunc(raw, unicode.IsSpace)
	if index < 0 {
		return parsedCommand{name: strings.ToLower(raw), ok: true}, nil
	}
	rawArgs := strings.TrimSpace(raw[index:])
	args, parseErr := parseArguments(rawArgs, len(prefix)+index+1)
	return parsedCommand{
		name:    strings.ToLower(raw[:index]),
		args:    args,
		rawArgs: rawArgs,
		ok:      true,
	}, parseErr
}

func parseArguments(input string, baseOffset int) ([]string, *CommandParseError) {
	var (
		args      []string
		builder   strings.Builder
		quote     byte
		inArg     bool
		quoteOpen int
	)

	flush := func() {
		if inArg {
			args = append(args, builder.String())
			builder.Reset()
			inArg = false
		}
	}

	for index := 0; index < len(input); {
		current := input[index]

		if quote == 0 && isSpaceByte(current) {
			flush()
			index++
			continue
		}

		if current == '\\' && quote != '\'' {
			inArg = true
			if index+1 >= len(input) {
				return nil, &CommandParseError{
					Offset: baseOffset + index,
					Reason: "trailing escape",
				}
			}
			index++
			builder.WriteByte(input[index])
			index++
			continue
		}

		if current == '\'' || current == '"' {
			if quote == 0 {
				quote = current
				quoteOpen = index
				inArg = true
				index++
				continue
			}
			if quote == current {
				quote = 0
				index++
				continue
			}
		}

		inArg = true
		builder.WriteByte(current)
		index++
	}

	if quote != 0 {
		return nil, &CommandParseError{
			Offset: baseOffset + quoteOpen,
			Reason: "unterminated quote",
		}
	}
	flush()
	return args, nil
}

func isSpaceByte(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	default:
		return false
	}
}

func normalizeCommandName(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", errors.New("mmbot: command name is empty")
	}
	for _, r := range value {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_') {
			return "", fmt.Errorf("mmbot: invalid command name %q", value)
		}
	}
	return value, nil
}
