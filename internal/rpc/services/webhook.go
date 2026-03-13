package services

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/webhook"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Compile-time check that WebhookService implements the interface
var _ discopanelv1connect.WebhookServiceHandler = (*WebhookService)(nil)

// WebhookService implements the Webhook service
type WebhookService struct {
	store          *storage.Store
	webhookManager *webhook.Manager
	log            *logger.Logger
}

// NewWebhookService creates a new webhook service
func NewWebhookService(store *storage.Store, webhookManager *webhook.Manager, log *logger.Logger) *WebhookService {
	return &WebhookService{
		store:          store,
		webhookManager: webhookManager,
		log:            log,
	}
}

// dbWebhookEventTypeToProto converts database webhook event type to proto
func dbWebhookEventTypeToProto(e string) v1.WebhookEventType {
	switch storage.WebhookEventType(e) {
	case storage.WebhookEventServerStart:
		return v1.WebhookEventType_WEBHOOK_EVENT_TYPE_SERVER_START
	case storage.WebhookEventServerStop:
		return v1.WebhookEventType_WEBHOOK_EVENT_TYPE_SERVER_STOP
	case storage.WebhookEventServerRestart:
		return v1.WebhookEventType_WEBHOOK_EVENT_TYPE_SERVER_RESTART
	default:
		return v1.WebhookEventType_WEBHOOK_EVENT_TYPE_UNSPECIFIED
	}
}

// protoWebhookEventTypeToDB converts proto webhook event type to database string
func protoWebhookEventTypeToDB(e v1.WebhookEventType) string {
	switch e {
	case v1.WebhookEventType_WEBHOOK_EVENT_TYPE_SERVER_START:
		return string(storage.WebhookEventServerStart)
	case v1.WebhookEventType_WEBHOOK_EVENT_TYPE_SERVER_STOP:
		return string(storage.WebhookEventServerStop)
	case v1.WebhookEventType_WEBHOOK_EVENT_TYPE_SERVER_RESTART:
		return string(storage.WebhookEventServerRestart)
	default:
		return ""
	}
}

// dbWebhookFormatToProto converts database webhook format to proto
func dbWebhookFormatToProto(f storage.WebhookFormat) v1.WebhookFormat {
	switch f {
	case storage.WebhookFormatGeneric:
		return v1.WebhookFormat_WEBHOOK_FORMAT_GENERIC
	case storage.WebhookFormatDiscord:
		return v1.WebhookFormat_WEBHOOK_FORMAT_DISCORD
	default:
		return v1.WebhookFormat_WEBHOOK_FORMAT_GENERIC
	}
}

// protoWebhookFormatToDB converts proto webhook format to database
func protoWebhookFormatToDB(f v1.WebhookFormat) storage.WebhookFormat {
	switch f {
	case v1.WebhookFormat_WEBHOOK_FORMAT_GENERIC:
		return storage.WebhookFormatGeneric
	case v1.WebhookFormat_WEBHOOK_FORMAT_DISCORD:
		return storage.WebhookFormatDiscord
	default:
		return storage.WebhookFormatGeneric
	}
}

// webhookToProto converts a database Webhook to proto
func webhookToProto(w *storage.Webhook) *v1.Webhook {
	events := make([]v1.WebhookEventType, len(w.Events))
	for i, e := range w.Events {
		events[i] = dbWebhookEventTypeToProto(e)
	}

	return &v1.Webhook{
		Id:           w.ID,
		ServerId:     w.ServerID,
		Name:         w.Name,
		Url:          w.URL,
		HasSecret:    w.Secret != "",
		Events:       events,
		Enabled:      w.Enabled,
		Format:       dbWebhookFormatToProto(w.Format),
		MaxRetries:   int32(w.MaxRetries),
		RetryDelayMs: int32(w.RetryDelayMs),
		TimeoutMs:    int32(w.TimeoutMs),
		Headers:      w.Headers,
		CreatedAt:    timestamppb.New(w.CreatedAt),
		UpdatedAt:    timestamppb.New(w.UpdatedAt),
	}
}

// ListWebhooks returns all webhooks for a server
func (s *WebhookService) ListWebhooks(ctx context.Context, req *connect.Request[v1.ListWebhooksRequest]) (*connect.Response[v1.ListWebhooksResponse], error) {
	webhooks, err := s.store.ListWebhooksByServer(ctx, req.Msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list webhooks: %w", err))
	}

	protoWebhooks := make([]*v1.Webhook, len(webhooks))
	for i, w := range webhooks {
		protoWebhooks[i] = webhookToProto(w)
	}

	return connect.NewResponse(&v1.ListWebhooksResponse{
		Webhooks: protoWebhooks,
	}), nil
}

// GetWebhook returns a specific webhook
func (s *WebhookService) GetWebhook(ctx context.Context, req *connect.Request[v1.GetWebhookRequest]) (*connect.Response[v1.GetWebhookResponse], error) {
	webhook, err := s.store.GetWebhook(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("webhook not found: %w", err))
	}

	return connect.NewResponse(&v1.GetWebhookResponse{
		Webhook: webhookToProto(webhook),
	}), nil
}

// CreateWebhook creates a new webhook
func (s *WebhookService) CreateWebhook(ctx context.Context, req *connect.Request[v1.CreateWebhookRequest]) (*connect.Response[v1.CreateWebhookResponse], error) {
	// Validate required fields
	if req.Msg.ServerId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("server_id is required"))
	}
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}
	if req.Msg.Url == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("url is required"))
	}
	if len(req.Msg.Events) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("at least one event is required"))
	}

	// Verify server exists
	_, err := s.store.GetServer(ctx, req.Msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found: %w", err))
	}

	// Convert events to strings
	events := make([]string, len(req.Msg.Events))
	for i, e := range req.Msg.Events {
		events[i] = protoWebhookEventTypeToDB(e)
	}

	// Create webhook with defaults
	maxRetries := int(req.Msg.MaxRetries)
	if maxRetries <= 0 {
		maxRetries = 3
	}
	retryDelayMs := int(req.Msg.RetryDelayMs)
	if retryDelayMs <= 0 {
		retryDelayMs = 1000
	}
	timeoutMs := int(req.Msg.TimeoutMs)
	if timeoutMs <= 0 {
		timeoutMs = 5000
	}

	webhook := &storage.Webhook{
		ID:           uuid.New().String(),
		ServerID:     req.Msg.ServerId,
		Name:         req.Msg.Name,
		URL:          req.Msg.Url,
		Secret:       req.Msg.Secret,
		Events:       events,
		Enabled:      true,
		Format:       protoWebhookFormatToDB(req.Msg.Format),
		MaxRetries:   maxRetries,
		RetryDelayMs: retryDelayMs,
		TimeoutMs:    timeoutMs,
		Headers:      req.Msg.Headers,
	}

	if err := s.store.CreateWebhook(ctx, webhook); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create webhook: %w", err))
	}

	s.log.Info("Webhook created: %s for server %s", webhook.Name, webhook.ServerID)

	return connect.NewResponse(&v1.CreateWebhookResponse{
		Webhook: webhookToProto(webhook),
	}), nil
}

// UpdateWebhook updates an existing webhook
func (s *WebhookService) UpdateWebhook(ctx context.Context, req *connect.Request[v1.UpdateWebhookRequest]) (*connect.Response[v1.UpdateWebhookResponse], error) {
	webhook, err := s.store.GetWebhook(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("webhook not found: %w", err))
	}

	// Update fields if provided
	if req.Msg.Name != nil {
		webhook.Name = *req.Msg.Name
	}
	if req.Msg.Url != nil {
		webhook.URL = *req.Msg.Url
	}
	if req.Msg.Secret != nil {
		webhook.Secret = *req.Msg.Secret
	}
	if len(req.Msg.Events) > 0 {
		events := make([]string, len(req.Msg.Events))
		for i, e := range req.Msg.Events {
			events[i] = protoWebhookEventTypeToDB(e)
		}
		webhook.Events = events
	}
	if req.Msg.Format != nil {
		webhook.Format = protoWebhookFormatToDB(*req.Msg.Format)
	}
	if req.Msg.MaxRetries != nil {
		webhook.MaxRetries = int(*req.Msg.MaxRetries)
	}
	if req.Msg.RetryDelayMs != nil {
		webhook.RetryDelayMs = int(*req.Msg.RetryDelayMs)
	}
	if req.Msg.TimeoutMs != nil {
		webhook.TimeoutMs = int(*req.Msg.TimeoutMs)
	}

	// Handle headers
	if req.Msg.ClearHeaders {
		webhook.Headers = make(map[string]string)
	}
	if len(req.Msg.Headers) > 0 {
		if webhook.Headers == nil {
			webhook.Headers = make(map[string]string)
		}
		for k, v := range req.Msg.Headers {
			webhook.Headers[k] = v
		}
	}

	if err := s.store.UpdateWebhook(ctx, webhook); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update webhook: %w", err))
	}

	s.log.Info("Webhook updated: %s", webhook.Name)

	return connect.NewResponse(&v1.UpdateWebhookResponse{
		Webhook: webhookToProto(webhook),
	}), nil
}

// DeleteWebhook deletes a webhook
func (s *WebhookService) DeleteWebhook(ctx context.Context, req *connect.Request[v1.DeleteWebhookRequest]) (*connect.Response[v1.DeleteWebhookResponse], error) {
	webhook, err := s.store.GetWebhook(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("webhook not found: %w", err))
	}

	if err := s.store.DeleteWebhook(ctx, req.Msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete webhook: %w", err))
	}

	s.log.Info("Webhook deleted: %s", webhook.Name)

	return connect.NewResponse(&v1.DeleteWebhookResponse{}), nil
}

// ToggleWebhook toggles webhook enabled/disabled status
func (s *WebhookService) ToggleWebhook(ctx context.Context, req *connect.Request[v1.ToggleWebhookRequest]) (*connect.Response[v1.ToggleWebhookResponse], error) {
	webhook, err := s.store.GetWebhook(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("webhook not found: %w", err))
	}

	webhook.Enabled = req.Msg.Enabled

	if err := s.store.UpdateWebhook(ctx, webhook); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to toggle webhook: %w", err))
	}

	status := "enabled"
	if !webhook.Enabled {
		status = "disabled"
	}
	s.log.Info("Webhook %s: %s", status, webhook.Name)

	return connect.NewResponse(&v1.ToggleWebhookResponse{
		Webhook: webhookToProto(webhook),
	}), nil
}

// TestWebhook sends a test payload to the webhook
func (s *WebhookService) TestWebhook(ctx context.Context, req *connect.Request[v1.TestWebhookRequest]) (*connect.Response[v1.TestWebhookResponse], error) {
	wh, err := s.store.GetWebhook(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("webhook not found: %w", err))
	}

	// Get the server for the test payload
	server, err := s.store.GetServer(ctx, wh.ServerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found: %w", err))
	}

	// Send test webhook
	result := s.webhookManager.TestWebhook(ctx, wh, server)

	return connect.NewResponse(&v1.TestWebhookResponse{
		Success:      result.Success,
		ResponseCode: int32(result.ResponseCode),
		ResponseBody: result.ResponseBody,
		ErrorMessage: result.ErrorMessage,
		DurationMs:   result.DurationMs,
	}), nil
}
