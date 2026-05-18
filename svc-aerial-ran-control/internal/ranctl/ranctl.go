// Package ranctl pulls Open5GS NF metrics + MongoDB subscriber counts and
// exposes a normalized JSON view of the radio plane.
package ranctl

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/respond"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Status is the response payload for GET /v1/ran/status.
type Status struct {
	Now              time.Time         `json:"now"`
	PLMN             string            `json:"plmn"`
	NFs              map[string]NFInfo `json:"nfs"`
	Subscribers      int64             `json:"subscribers"`
	OpenSessions     int64             `json:"open_sessions,omitempty"`
	ScrapeDurationMS int64             `json:"scrape_duration_ms"`
}

// NFInfo summarizes one network function.
type NFInfo struct {
	Reachable bool   `json:"reachable"`
	MetricsURL string `json:"metrics_url,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Service holds dependencies.
type Service struct {
	http       *http.Client
	mongo      *mongo.Client
	mongoDB    string
	nfEndpoints map[string]string // name → http://host:port/metrics
	plmn       string
}

// New wires the service. nfEndpoints is e.g. {"amf":"http://open5gs-amf:9090/metrics", ...}
func New(ctx context.Context, mongoURI, mongoDB, plmn string, nfEndpoints map[string]string) (*Service, error) {
	cli, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := cli.Ping(pctx, nil); err != nil {
		return nil, err
	}
	return &Service{
		http:        &http.Client{Timeout: 3 * time.Second},
		mongo:       cli,
		mongoDB:     mongoDB,
		nfEndpoints: nfEndpoints,
		plmn:        plmn,
	}, nil
}

// Status probes each NF and the MongoDB subscriber count.
func (s *Service) Status(ctx context.Context) (*Status, error) {
	start := time.Now()
	st := &Status{Now: start, PLMN: s.plmn, NFs: map[string]NFInfo{}}

	for name, url := range s.nfEndpoints {
		info := NFInfo{MetricsURL: url}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		res, err := s.http.Do(req)
		if err != nil {
			info.Reachable = false
			info.Error = err.Error()
		} else {
			info.Reachable = res.StatusCode == 200
			if !info.Reachable {
				info.Error = res.Status
			}
			_, _ = io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}
		st.NFs[name] = info
	}

	if s.mongo != nil {
		n, err := s.mongo.Database(s.mongoDB).Collection("subscribers").CountDocuments(ctx, bson.M{})
		if err == nil {
			st.Subscribers = n
		}
	}
	st.ScrapeDurationMS = time.Since(start).Milliseconds()
	return st, nil
}

// Subscribers lists all IMSI in MongoDB (helper for the UI).
func (s *Service) Subscribers(ctx context.Context) ([]string, error) {
	if s.mongo == nil {
		return nil, errors.New("mongo not configured")
	}
	cur, err := s.mongo.Database(s.mongoDB).Collection("subscribers").Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{"imsi": 1, "_id": 0}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := []string{}
	for cur.Next(ctx) {
		var d struct{ IMSI string `bson:"imsi"` }
		if err := cur.Decode(&d); err == nil {
			out = append(out, d.IMSI)
		}
	}
	return out, nil
}

// Handler exposes endpoints.
type Handler struct{ svc *Service }

func NewHandler(s *Service) *Handler { return &Handler{svc: s} }

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/ran/status", h.status)
	mux.HandleFunc("GET /v1/ran/subscribers", h.subs)
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.Status(r.Context())
	if err != nil {
		respond.DBError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, st)
}

func (h *Handler) subs(w http.ResponseWriter, r *http.Request) {
	subs, err := h.svc.Subscribers(r.Context())
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "mongo_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, subs)
}

// ParseNFList parses "amf=http://host:9090/metrics,smf=http://h:9090/metrics".
func ParseNFList(csv string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(csv, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		i := strings.IndexByte(pair, '=')
		if i <= 0 {
			continue
		}
		out[strings.TrimSpace(pair[:i])] = strings.TrimSpace(pair[i+1:])
	}
	return out
}

// silence unused encoding/json on builds with no struct decoding (helper kept).
var _ = json.Marshal
