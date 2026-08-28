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
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-ran-control/internal/gnb"
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
	// SubscribersError is set when the subscriber count could not be read, so a
	// caller can tell "0 subscribers" apart from "the count query failed".
	SubscribersError string            `json:"subscribers_error,omitempty"`
	OpenSessions     int64             `json:"open_sessions,omitempty"`
	ScrapeDurationMS int64             `json:"scrape_duration_ms"`
}

// NFInfo summarizes one network function.
type NFInfo struct {
	Reachable bool   `json:"reachable"`
	MetricsURL string `json:"metrics_url,omitempty"`
	Error     string `json:"error,omitempty"`
}

// subscriberCounter reads the number of provisioned subscribers from the 5G
// core. Abstracted so Status's error handling is testable without a live Mongo.
type subscriberCounter interface {
	Count(ctx context.Context) (int64, error)
}

// mongoCounter is the production subscriberCounter over Open5GS's MongoDB.
type mongoCounter struct{ coll *mongo.Collection }

func (m *mongoCounter) Count(ctx context.Context) (int64, error) {
	return m.coll.CountDocuments(ctx, bson.M{})
}

// Service holds dependencies.
type Service struct {
	http        *http.Client
	mongo       *mongo.Client
	mongoDB     string
	counter     subscriberCounter // nil when no 5G core reachable
	nfEndpoints map[string]string // name → http://host:port/metrics
	plmn        string
}

// New wires the service. nfEndpoints is e.g. {"amf":"http://open5gs-amf:9090/metrics", ...}.
//
// Mongo is best-effort: if Open5GS's MongoDB is unreachable at boot (it lives in
// a different namespace and may lag), the service still starts. Status/Subscribers
// report the outage instead of taking down the whole service (and its GNodeB API).
func New(ctx context.Context, mongoURI, mongoDB, plmn string, nfEndpoints map[string]string) (*Service, error) {
	s := &Service{
		http:        &http.Client{Timeout: 3 * time.Second},
		mongoDB:     mongoDB,
		nfEndpoints: nfEndpoints,
		plmn:        plmn,
	}
	cli, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err == nil {
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if pingErr := cli.Ping(pctx, nil); pingErr == nil {
			s.mongo = cli
			s.counter = &mongoCounter{coll: cli.Database(mongoDB).Collection("subscribers")}
		}
	}
	return s, nil
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

	if s.counter != nil {
		n, err := s.counter.Count(ctx)
		if err != nil {
			// Don't silently report 0 — that reads as "no SIMs" when the query
			// actually failed. Surface it like the per-NF errors above.
			st.SubscribersError = err.Error()
		} else {
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

// Handler exposes endpoints. gnb is optional: when nil, the GNodeB routes
// return 503 (e.g. running host-bound with no cluster access).
type Handler struct {
	svc *Service
	gnb *gnb.Client
}

func NewHandler(s *Service) *Handler { return &Handler{svc: s} }

// WithGNB attaches a wavekube GNodeB client so the service can declaratively
// request GPU-accelerated cells.
func (h *Handler) WithGNB(c *gnb.Client) *Handler { h.gnb = c; return h }

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/ran/status", h.status)
	mux.HandleFunc("GET /v1/ran/subscribers", h.subs)
	// wavekube GNodeB (declarative RAN) — the Phase-3 bridge.
	mux.HandleFunc("POST /v1/ran/gnodebs", h.createGNB)
	mux.HandleFunc("GET /v1/ran/gnodebs", h.listGNB)
	mux.HandleFunc("GET /v1/ran/gnodebs/{name}", h.getGNB)
	mux.HandleFunc("DELETE /v1/ran/gnodebs/{name}", h.deleteGNB)
}

func (h *Handler) createGNB(w http.ResponseWriter, r *http.Request) {
	if h.gnb == nil {
		respond.Error(w, http.StatusServiceUnavailable, "gnb_unavailable", "no cluster access; GNodeB API disabled")
		return
	}
	var req gnb.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid json")
		return
	}
	g, err := h.gnb.Create(r.Context(), req)
	if err != nil {
		if gnb.IsInvalidRequest(err) {
			respond.Error(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if gnb.IsAlreadyExists(err) {
			respond.Error(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		respond.Error(w, http.StatusBadGateway, "k8s_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusCreated, g)
}

func (h *Handler) listGNB(w http.ResponseWriter, r *http.Request) {
	if h.gnb == nil {
		respond.Error(w, http.StatusServiceUnavailable, "gnb_unavailable", "no cluster access; GNodeB API disabled")
		return
	}
	gs, err := h.gnb.List(r.Context())
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "k8s_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, gs)
}

func (h *Handler) getGNB(w http.ResponseWriter, r *http.Request) {
	if h.gnb == nil {
		respond.Error(w, http.StatusServiceUnavailable, "gnb_unavailable", "no cluster access; GNodeB API disabled")
		return
	}
	g, err := h.gnb.Get(r.Context(), r.PathValue("name"))
	if err != nil {
		if gnb.IsNotFound(err) {
			respond.Error(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		respond.Error(w, http.StatusBadGateway, "k8s_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, g)
}

func (h *Handler) deleteGNB(w http.ResponseWriter, r *http.Request) {
	if h.gnb == nil {
		respond.Error(w, http.StatusServiceUnavailable, "gnb_unavailable", "no cluster access; GNodeB API disabled")
		return
	}
	if err := h.gnb.Delete(r.Context(), r.PathValue("name")); err != nil {
		if gnb.IsNotFound(err) {
			respond.Error(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		respond.Error(w, http.StatusBadGateway, "k8s_error", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
