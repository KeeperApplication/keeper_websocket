package handler

import (
	"log/slog"
	"net/http"
	"net/url"

	"github.com/gorilla/websocket"
	"keeper.websocket.go/internal/auth"
	"keeper.websocket.go/internal/config"
	internalWs "keeper.websocket.go/internal/websocket"
)

type WebsocketHandler struct {
	hub         *internalWs.Hub
	logger      *slog.Logger
	cfg         *config.Config
	authorizer  *auth.Authorizer
	publishFunc internalWs.PublishFunc
}

func NewWebsocketHandler(h *internalWs.Hub, l *slog.Logger, cfg *config.Config, auth *auth.Authorizer, pub internalWs.PublishFunc) *WebsocketHandler {
	return &WebsocketHandler{
		hub:         h,
		logger:      l,
		cfg:         cfg,
		authorizer:  auth,
		publishFunc: pub,
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

	username, err := auth.ValidateToken(token, wh.cfg.JWTSecret)
	if err != nil {
		wh.logger.Warn("invalid token", "error", err)
		http.Error(w, "Invalid auth token", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		wh.logger.Error("failed to upgrade connection", "error", err)
		return
	}

	client := internalWs.NewClient(wh.hub, conn, wh.logger, username, token, wh.authorizer, wh.publishFunc)
	client.Hub.Register <- client

	wh.logger.Info("client connected and authenticated", "user", username)

	go client.WritePump()
	go client.ReadPump()
}
