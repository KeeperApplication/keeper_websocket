package coreapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"keeper.websocket.go/internal/config"
)

type Client struct {
	httpClient *http.Client
	cfg        *config.Config
	logger     *slog.Logger
}

func NewClient(cfg *config.Config, logger *slog.Logger) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		cfg:        cfg,
		logger:     logger,
	}
}

func (c *Client) PostNewMessage(userToken, roomID, content string) error {
	url := fmt.Sprintf("%s/messages/rooms/%s/new", c.cfg.JavaApiURL, roomID)
	payload := map[string]string{"content": content}
	return c.doPost(userToken, url, payload)
}

func (c *Client) EditMessage(userToken, messageID, content string) error {
	url := fmt.Sprintf("%s/messages/%s", c.cfg.JavaApiURL, messageID)
	payload := map[string]string{"content": content}
	return c.doPut(userToken, url, payload)
}

func (c *Client) DeleteMessage(userToken, messageID string) error {
	url := fmt.Sprintf("%s/messages/%s", c.cfg.JavaApiURL, messageID)
	return c.doDelete(userToken, url)
}

func (c *Client) TogglePinMessage(userToken, messageID string) error {
	url := fmt.Sprintf("%s/messages/%s/toggle-pin", c.cfg.JavaApiURL, messageID)
	return c.doPost(userToken, url, nil)
}

func (c *Client) ToggleReaction(userToken, messageID, emoji string) error {
	url := fmt.Sprintf("%s/messages/%s/reactions", c.cfg.JavaApiURL, messageID)
	return c.doPost(userToken, url, emoji)
}

func (c *Client) MarkMessagesAsSeen(userToken, roomID string, lastMessageID float64) error {
	url := fmt.Sprintf("%s/messages/rooms/%s/seen", c.cfg.JavaApiURL, roomID)
	payload := map[string]interface{}{"lastMessageId": lastMessageID}
	return c.doPost(userToken, url, payload)
}

func (c *Client) doRequest(method, url, userToken string, payload interface{}) error {
	var reqBody []byte
	var err error

	if payload != nil {
		reqBody, err = json.Marshal(payload)
		if err != nil {
			c.logger.Error("failed to marshal payload", "error", err)
			return err
		}
	}

	req, err := http.NewRequest(method, url, bytes.NewBuffer(reqBody))
	if err != nil {
		c.logger.Error("failed to create request", "error", err)
		return err
	}

	req.Header.Set("Authorization", "Bearer "+userToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "keeper-websocket-go/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("failed to perform request", "url", url, "error", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		c.logger.Warn("api request failed", "url", url, "status", resp.StatusCode)
		return fmt.Errorf("API returned non-2xx status: %d", resp.StatusCode)
	}

	return nil
}

func (c *Client) doPost(userToken, url string, payload interface{}) error {
	return c.doRequest("POST", url, userToken, payload)
}

func (c *Client) doPut(userToken, url string, payload interface{}) error {
	return c.doRequest("PUT", url, userToken, payload)
}

func (c *Client) doDelete(userToken, url string) error {
	return c.doRequest("DELETE", url, userToken, nil)
}
