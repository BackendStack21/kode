package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/BackendStack21/kode/internal/session"
	"github.com/BackendStack21/kode/internal/telegram"
)

// telegramCmd is the entry point for "odek telegram".
func telegramCmd(args []string) error {
	// 1. Load and validate config.
	cfg := telegram.ConfigFromEnv()
	if err := telegram.ValidateConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "odek telegram: %v\n", err)
		return err
	}

	// 2. Create bot client.
	bot := telegram.NewBot(cfg.Token)

	// 3. Create session store on disk (~/.odek/sessions/).
	store, err := session.NewStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "odek telegram: session store: %v\n", err)
		return err
	}

	// 4. Create session manager (per-chat Telegram session cache).
	sessionManager := telegram.NewSessionManager(store)
	_ = sessionManager // will be used when the agent engine is wired in

	// 5. Create handler.
	handler := telegram.NewHandler(bot)

	// 6. Set handler config from cfg.
	handler.Config = telegram.HandlerConfig{
		AllowedChats: cfg.AllowedChats,
		BotUsername:  cfg.BotUsername,
		MaxMsgLength: cfg.MaxMsgLength,
		AllowedUsers: cfg.AllowedUsers,
	}

	// 7. Wire handler callbacks.
	handler.OnTextMessage = func(chatID int64, text string) (string, error) {
		// Placeholder: load session, append user message, run agent,
		// save session, return response.
		// For now, just echo the text back as a placeholder.
		return fmt.Sprintf("Echo: %s", text), nil
	}

	handler.OnCommand = func(chatID int64, cmdName string, argsStr string) (string, error) {
		cmd := telegram.FindCommand(cmdName)
		if cmd == nil {
			return fmt.Sprintf("Unknown command: /%s", cmdName), nil
		}
		return cmd.Handler(argsStr)
	}

	handler.OnCallbackQuery = func(chatID int64, data string) (string, error) {
		return fmt.Sprintf("Callback received: %s", data), nil
	}

	handler.OnError = func(chatID int64, err error) {
		fmt.Fprintf(os.Stderr, "odek telegram: error for chat %d: %v\n", chatID, err)
	}

	// 8. Print startup banner.
	fmt.Fprintf(os.Stderr, "odek telegram bot started\n")

	// 9. Create poller.
	poller := telegram.NewPoller(bot)
	poller.Interval = time.Duration(cfg.PollInterval) * time.Second
	poller.Timeout = cfg.PollTimeout

	// 10. Create cancellable context for graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 11. Handle SIGINT/SIGTERM for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintf(os.Stderr, "\nodek telegram: shutting down...\n")
		cancel()
	}()

	// 12. Start polling in a background goroutine.
	updates := make(chan telegram.Update, 100)
	go poller.Start(ctx, updates)

	// 13. Process updates until the channel is closed (ctx cancelled).
	for upd := range updates {
		handler.HandleUpdate(upd)
	}

	return nil
}
