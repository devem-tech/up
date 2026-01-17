package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type NotifyFunc func(ctx context.Context, msg string) error

type telegramNotifier struct {
	token  string
	chatID string
	client *http.Client
}

func NewTelegramNotifierFromEnv() (NotifyFunc, error) {
	token := strings.TrimSpace(os.Getenv("TELEGRAM_API_TOKEN"))
	if token == "" {
		return nil, nil
	}
	chatID := strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID"))
	if chatID == "" {
		return nil, fmt.Errorf("TELEGRAM_API_TOKEN set but chat id is missing")
	}

	n := telegramNotifier{
		token:  token,
		chatID: chatID,
		client: &http.Client{Timeout: 10 * time.Second},
	}
	return n.Notify, nil
}

func (n telegramNotifier) Notify(ctx context.Context, msg string) error {
	form := url.Values{}
	form.Set("chat_id", n.chatID)
	form.Set("text", msg)
	form.Set("parse_mode", "HTML")

	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("telegram status %s: %s", resp.Status, strings.TrimSpace(string(body)))
}
