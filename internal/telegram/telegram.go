// Package telegram is a minimal client for the few Bot API methods floodwatch
// uses, written against https://core.telegram.org/bots/api.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const apiBase = "https://api.telegram.org"

type Client struct {
	http  *http.Client
	base  string
	token string
}

func New(token string, httpClient *http.Client) *Client {
	return newClient(apiBase, token, httpClient)
}

func newClient(base, token string, httpClient *http.Client) *Client {
	return &Client{http: httpClient, base: strings.TrimSuffix(base, "/"), token: token}
}

// APIError is a request Telegram answered and refused.
type APIError struct {
	Method      string
	Code        int
	Description string
	RetryAfter  int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram %s: %d %s", e.Method, e.Code, e.Description)
}

// Blocked reports whether the recipient has blocked the bot or deleted their
// account, after which nothing can ever be delivered to them.
func Blocked(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == http.StatusForbidden
}

// Unauthorized reports whether the token itself was rejected.
func Unauthorized(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == http.StatusUnauthorized
}

func (c *Client) call(ctx context.Context, method string, params, result any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("telegram %s: encode: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/bot"+c.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return c.redact(fmt.Errorf("telegram %s: %w", method, err))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return c.redact(fmt.Errorf("telegram %s: %w", method, err))
	}
	defer resp.Body.Close()

	var envelope struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		ErrorCode   int             `json:"error_code"`
		Description string          `json:"description"`
		Parameters  struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&envelope); err != nil {
		return fmt.Errorf("telegram %s: status %d, undecodable body: %w", method, resp.StatusCode, err)
	}
	if !envelope.OK {
		code := envelope.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		return &APIError{Method: method, Code: code, Description: envelope.Description, RetryAfter: envelope.Parameters.RetryAfter}
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(envelope.Result, result); err != nil {
		return fmt.Errorf("telegram %s: decode result: %w", method, err)
	}
	return nil
}

// redact strips the token from transport errors, which quote the request URL
// and would otherwise put the token into logs.
func (c *Client) redact(err error) error {
	if c.token == "" {
		return err
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		urlErr.URL = strings.ReplaceAll(urlErr.URL, c.token, "<token>")
	}
	if !strings.Contains(err.Error(), c.token) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), c.token, "<token>"))
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type Message struct {
	MessageID      int64     `json:"message_id"`
	From           *User     `json:"from"`
	Chat           Chat      `json:"chat"`
	Text           string    `json:"text"`
	Location       *Location `json:"location"`
	ReplyToMessage *Message  `json:"reply_to_message"`
}

type User struct {
	ID    int64 `json:"id"`
	IsBot bool  `json:"is_bot"`
	// LanguageCode is the IETF tag of the user's app language, like "th";
	// Telegram may leave it empty.
	LanguageCode string `json:"language_code"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type Location struct {
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	LivePeriod int     `json:"live_period"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from"`
	Data    string   `json:"data"`
	Message *Message `json:"message"`
}

// GetUpdates long-polls for up to timeoutSeconds. Updates before offset are
// confirmed and never returned again.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSeconds int) ([]Update, error) {
	var updates []Update
	err := c.call(ctx, "getUpdates", map[string]any{
		"offset":          offset,
		"timeout":         timeoutSeconds,
		"allowed_updates": []string{"message", "callback_query"},
	}, &updates)
	return updates, err
}

type ReplyKeyboard struct {
	Keyboard        [][]KeyboardButton `json:"keyboard"`
	ResizeKeyboard  bool               `json:"resize_keyboard,omitempty"`
	OneTimeKeyboard bool               `json:"one_time_keyboard,omitempty"`
}

type KeyboardButton struct {
	Text            string `json:"text"`
	RequestLocation bool   `json:"request_location,omitempty"`
}

type InlineKeyboard struct {
	InlineKeyboard [][]InlineButton `json:"inline_keyboard"`
}

type InlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

// ForceReply opens the reply box on the message it is sent with, so the
// user's answer arrives quoting it.
type ForceReply struct {
	ForceReply            bool   `json:"force_reply"`
	InputFieldPlaceholder string `json:"input_field_placeholder,omitempty"`
}

type OutgoingMessage struct {
	ChatID      int64  `json:"chat_id"`
	Text        string `json:"text"`
	ParseMode   string `json:"parse_mode,omitempty"`
	ReplyMarkup any    `json:"reply_markup,omitempty"`

	LinkPreview *LinkPreviewOptions `json:"link_preview_options,omitempty"`
}

type LinkPreviewOptions struct {
	IsDisabled bool `json:"is_disabled"`
}

func (c *Client) SendMessage(ctx context.Context, m OutgoingMessage) error {
	if m.LinkPreview == nil {
		m.LinkPreview = &LinkPreviewOptions{IsDisabled: true}
	}
	return c.call(ctx, "sendMessage", m, nil)
}

// SendVenue sends a named map pin, which opens in the phone's own maps app.
func (c *Client) SendVenue(ctx context.Context, chatID int64, lat, lng float64, title, address string) error {
	return c.call(ctx, "sendVenue", map[string]any{
		"chat_id": chatID, "latitude": lat, "longitude": lng, "title": title, "address": address,
	}, nil)
}

// EditMessageText replaces a message's text and drops its inline buttons.
func (c *Client) EditMessageText(ctx context.Context, chatID, messageID int64, text, parseMode string) error {
	params := map[string]any{"chat_id": chatID, "message_id": messageID, "text": text}
	if parseMode != "" {
		params["parse_mode"] = parseMode
	}
	return c.call(ctx, "editMessageText", params, nil)
}

func (c *Client) AnswerCallbackQuery(ctx context.Context, id string) error {
	return c.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id}, nil)
}

type Command struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// SetMyCommands sets the command menu shown to users whose app language is
// languageCode, or to everyone else when it is empty.
func (c *Client) SetMyCommands(ctx context.Context, commands []Command, languageCode string) error {
	params := map[string]any{"commands": commands}
	if languageCode != "" {
		params["language_code"] = languageCode
	}
	return c.call(ctx, "setMyCommands", params, nil)
}
