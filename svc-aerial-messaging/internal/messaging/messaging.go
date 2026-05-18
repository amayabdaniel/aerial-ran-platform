// Package messaging is a tiny user-to-user message bus: HTTP POST writes a row
// + publishes to NATS JetStream subject "core.event.message.sent" (durable);
// GET history reads from Postgres; WebSocket /v1/stream tails the JetStream subject
// scoped to the JWT user (server-side filter).
package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/respond"
	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	subjectSent = "core.event.message.sent"
	streamName  = "AERIAL_MESSAGES"
)

type Message struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	FromUser  string    `json:"from_user_id"`
	ToUser    string    `json:"to_user_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type SendRequest struct {
	ToUserID string `json:"to_user_id"`
	Body     string `json:"body"`
}

// Service holds pool + JetStream.
type Service struct {
	pool *pgxpool.Pool
	js   jetstream.JetStream
}

// New connects to NATS, creates the stream if missing, returns the service.
func New(ctx context.Context, pool *pgxpool.Pool, natsURL string) (*Service, error) {
	nc, err := nats.Connect(natsURL)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      streamName,
		Subjects:  []string{"core.event.message.>"},
		Retention: jetstream.InterestPolicy,
		MaxAge:    7 * 24 * time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("ensure stream: %w", err)
	}
	return &Service{pool: pool, js: js}, nil
}

// Send persists + publishes.
func (s *Service) Send(ctx context.Context, orgID, fromUser string, req SendRequest) (*Message, error) {
	if req.ToUserID == "" || req.Body == "" {
		return nil, errors.New("to_user_id and body required")
	}
	m := &Message{OrgID: orgID, FromUser: fromUser, ToUser: req.ToUserID, Body: req.Body}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO messaging.messages(org_id, from_user_id, to_user_id, body)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4) RETURNING id::text, created_at`,
		m.OrgID, m.FromUser, m.ToUser, m.Body,
	).Scan(&m.ID, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(m)
	// Publish per-recipient subject so a server-side filter on the WS tail is trivial.
	_, _ = s.js.Publish(ctx, fmt.Sprintf("%s.%s", subjectSent, m.ToUser), payload)
	return m, nil
}

// Inbox returns the user's most recent inbound messages.
func (s *Service) Inbox(ctx context.Context, userID string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, org_id::text, from_user_id::text, to_user_id::text, body, created_at
		   FROM messaging.messages WHERE to_user_id = $1::uuid
		   ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.OrgID, &m.FromUser, &m.ToUser, &m.Body, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Handler exposes HTTP endpoints.
type Handler struct{ svc *Service }

func NewHandler(s *Service) *Handler { return &Handler{svc: s} }

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/messages", h.send)
	mux.HandleFunc("GET /v1/messages/inbox", h.inbox)
	mux.HandleFunc("GET /v1/messages/stream", h.stream)
}

func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
	claims, ok := jwt.FromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "no token")
		return
	}
	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid json")
		return
	}
	m, err := h.svc.Send(r.Context(), claims.OrgID, claims.UserID, req)
	if err != nil {
		respond.DBError(w, err)
		return
	}
	respond.JSON(w, http.StatusCreated, m)
}

func (h *Handler) inbox(w http.ResponseWriter, r *http.Request) {
	claims, _ := jwt.FromContext(r.Context())
	msgs, err := h.svc.Inbox(r.Context(), claims.UserID, 50)
	if err != nil {
		respond.DBError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, msgs)
}

// stream upgrades to WebSocket and pushes JetStream messages to the user.
func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
	claims, ok := jwt.FromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "no token")
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // dev only — origin checked at gateway in prod
	})
	if err != nil {
		return
	}
	defer c.CloseNow()

	subject := fmt.Sprintf("%s.%s", subjectSent, claims.UserID)
	cons, err := h.svc.js.OrderedConsumer(r.Context(), streamName, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{subject},
		DeliverPolicy:  jetstream.DeliverNewPolicy,
	})
	if err != nil {
		_ = c.Close(websocket.StatusInternalError, "consumer: "+err.Error())
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	it, err := cons.Messages()
	if err != nil {
		_ = c.Close(websocket.StatusInternalError, "messages: "+err.Error())
		return
	}
	defer it.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		msg, err := it.Next()
		if err != nil {
			return
		}
		if err := c.Write(ctx, websocket.MessageText, msg.Data()); err != nil {
			return
		}
		_ = msg.Ack()
	}
}
