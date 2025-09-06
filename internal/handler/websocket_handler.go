package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/gorilla/websocket"
	"keeper.websocket.go/internal/auth"
	"keeper.websocket.go/internal/config"
	"keeper.websocket.go/internal/presence"
	internalWs "keeper.websocket.go/internal/websocket"
)

type WebsocketHandler struct {
	hub             *internalWs.Hub
	logger          *slog.Logger
	cfg             *config.Config
	authorizer      *auth.Authorizer
	publishFunc     internalWs.PublishFunc
	presenceService *presence.Service
}

func NewWebsocketHandler(h *internalWs.Hub, l *slog.Logger, cfg *config.Config, auth *auth.Authorizer, pub internalWs.PublishFunc, pres *presence.Service) *WebsocketHandler {
	return &WebsocketHandler{
		hub:             h,
		logger:          l,
		cfg:             cfg,
		authorizer:      auth,
		publishFunc:     pub,
		presenceService: pres,
	}
}

func (wh *WebsocketHandler) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}

	return u.String() == wh.cfg.FrontendURL
}

func (wh *WebsocketHandler) ServeWs(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     wh.checkOrigin,
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		wh.logger.Warn("connection attempt without token")
		http.Error(w, "Missing auth token", http.StatusUnauthorized)
		return
	}

	claimsData, err := auth.ValidateToken(token, wh.cfg.JWTPublicKey)
	if err != nil {
		wh.logger.Warn("invalid token", "error", err)
		http.Error(w, "Invalid auth token", http.StatusUnauthorized)
		return
	}
	username := claimsData.Username

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		wh.logger.Error("failed to upgrade connection", "error", err)
		return
	}

	err = wh.presenceService.AddUser(context.Background(), username)
	if err != nil {
		wh.logger.Error("failed to add user to presence service", "error", err)
	}

	conn.SetCloseHandler(func(code int, text string) error {
		wh.logger.Info("client connection closed", "user", username, "code", code)
		err := wh.presenceService.RemoveUser(context.Background(), username)
		if err != nil {
			wh.logger.Error("failed to remove user from presence service on close", "user", username, "error", err)
		}
		return nil
	})

	roomPermissionMap := make(map[int64]bool)
	for _, id := range claimsData.RoomIDs {
		roomPermissionMap[id] = true
	}

	client := internalWs.NewClient(wh.hub, conn, wh.logger, username, token, wh.authorizer, wh.publishFunc, wh.presenceService, roomPermissionMap)
	client.Hub.Register <- client

	wh.logger.Info("client connected and authenticated", "user", username)

	go client.WritePump()
	go client.ReadPump()
}
