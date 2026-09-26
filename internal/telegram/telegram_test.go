package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Deliberately not shaped like a real token, so secret scanners stay quiet.
const testToken = "not-a-real-token-for-tests"

func fakeAPI(t *testing.T, handle func(method string, params map[string]any) string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := "/bot" + testToken + "/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			t.Errorf("path %q does not carry the token", r.URL.Path)
		}
		var params map[string]any
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &params); err != nil {
			t.Errorf("body is not JSON: %s", raw)
		}
		io.WriteString(w, handle(strings.TrimPrefix(r.URL.Path, prefix), params))
	}))
	t.Cleanup(srv.Close)
	return newClient(srv.URL, testToken, srv.Client())
}

func TestGetUpdates(t *testing.T) {
	c := fakeAPI(t, func(method string, params map[string]any) string {
		if method != "getUpdates" || params["offset"] != float64(7) || params["timeout"] != float64(50) {
			t.Errorf("%s %v", method, params)
		}
		return `{"ok":true,"result":[
			{"update_id":7,"message":{"message_id":1,"chat":{"id":42,"type":"private"},"location":{"latitude":13.6515,"longitude":100.4945}}},
			{"update_id":8,"callback_query":{"id":"cb1","data":"rm:Home","message":{"message_id":2,"chat":{"id":42,"type":"private"}}}}
		]}`
	})

	updates, err := c.GetUpdates(context.Background(), 7, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 2 {
		t.Fatalf("got %d updates", len(updates))
	}
	if loc := updates[0].Message.Location; loc == nil || loc.Latitude != 13.6515 || updates[0].Message.Chat.ID != 42 {
		t.Errorf("message = %+v", updates[0].Message)
	}
	if cq := updates[1].CallbackQuery; cq == nil || cq.Data != "rm:Home" || cq.Message.MessageID != 2 {
		t.Errorf("callback = %+v", updates[1].CallbackQuery)
	}
}

func TestSendMessageDisablesLinkPreviews(t *testing.T) {
	c := fakeAPI(t, func(method string, params map[string]any) string {
		preview, _ := params["link_preview_options"].(map[string]any)
		if method != "sendMessage" || params["chat_id"] != float64(42) || params["parse_mode"] != "HTML" || preview["is_disabled"] != true {
			t.Errorf("%s %v", method, params)
		}
		return `{"ok":true,"result":{"message_id":3,"chat":{"id":42,"type":"private"}}}`
	})
	if err := c.SendMessage(context.Background(), OutgoingMessage{ChatID: 42, Text: "hi", ParseMode: "HTML"}); err != nil {
		t.Fatal(err)
	}
}

func TestAPIErrors(t *testing.T) {
	c := fakeAPI(t, func(string, map[string]any) string {
		return `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`
	})
	err := c.SendMessage(context.Background(), OutgoingMessage{ChatID: 42, Text: "hi"})
	if !Blocked(err) || Unauthorized(err) {
		t.Errorf("err = %v, want Blocked", err)
	}

	c = fakeAPI(t, func(string, map[string]any) string {
		return `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":17}}`
	})
	err = c.SendMessage(context.Background(), OutgoingMessage{ChatID: 42, Text: "hi"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.RetryAfter != 17 {
		t.Errorf("err = %v, want retry_after 17", err)
	}
}

func TestTransportErrorsDoNotLeakTheToken(t *testing.T) {
	// Nothing listens on this port, so the request fails in the transport,
	// whose error quotes the full URL.
	c := newClient("http://127.0.0.1:1", testToken, http.DefaultClient)
	err := c.SendMessage(context.Background(), OutgoingMessage{ChatID: 42, Text: "hi"})
	if err == nil {
		t.Fatal("want a connection error")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("error leaks the token: %v", err)
	}
	if !strings.Contains(err.Error(), "<token>") {
		t.Errorf("error lost its URL context: %v", err)
	}
}
