// Package open5gs writes subscriber records to Open5GS's MongoDB so UEs can attach.
// The document shape mirrors what Open5GS WebUI / dbctl produces — verified by
// running `mongosh open5gs --eval 'printjson(db.subscribers.findOne())'` after
// the WebUI inserts.
package open5gs

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Client wraps a MongoDB connection scoped to the open5gs DB.
type Client struct {
	c    *mongo.Client
	coll *mongo.Collection
}

// New connects and pings.
func New(ctx context.Context, uri, db string) (*Client, error) {
	c, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	ctxPing, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.Ping(ctxPing, nil); err != nil {
		return nil, err
	}
	return &Client{c: c, coll: c.Database(db).Collection("subscribers")}, nil
}

// Close disconnects.
func (cl *Client) Close(ctx context.Context) error { return cl.c.Disconnect(ctx) }

// Subscriber is the document shape Open5GS expects. Many fields stay default
// for 5G-SA + single-slice; we expose only what we care about.
type Subscriber struct {
	IMSI string
	APN  string
	Ki   string // 32 hex chars
	OPc  string // 32 hex chars
	AMF  string // default "8000"
	SST  int16
	SD   string // optional; empty if AMF advertises sst-only
}

// Upsert inserts or updates the subscriber by IMSI.
func (cl *Client) Upsert(ctx context.Context, s Subscriber) error {
	amf := s.AMF
	if amf == "" {
		amf = "8000"
	}
	if s.APN == "" {
		s.APN = "internet"
	}
	if s.SST == 0 {
		s.SST = 1
	}

	slice := bson.M{
		"sst":               s.SST,
		"default_indicator": true,
		"session": []bson.M{{
			"name": s.APN,
			"type": 3, // IPv4
			"ambr": bson.M{
				"downlink": bson.M{"value": 1, "unit": 3},
				"uplink":   bson.M{"value": 1, "unit": 3},
			},
			"qos": bson.M{
				"index": 9,
				"arp": bson.M{
					"priority_level":          8,
					"pre_emption_capability":  1,
					"pre_emption_vulnerability": 1,
				},
			},
			"pcc_rule": []bson.M{},
		}},
	}
	if s.SD != "" {
		slice["sd"] = s.SD
	}

	doc := bson.M{
		"schema_version": 1,
		"imsi":           s.IMSI,
		"msisdn":         []string{},
		"imeisv":         []string{},
		"mme_host":       []string{},
		"mme_realm":      []string{},
		"purge_flag":     []bool{},
		"security": bson.M{
			"k":   s.Ki,
			"opc": s.OPc,
			"amf": amf,
			"op":  nil,
		},
		"ambr": bson.M{
			"downlink": bson.M{"value": 1, "unit": 3},
			"uplink":   bson.M{"value": 1, "unit": 3},
		},
		"slice":                       []bson.M{slice},
		"access_restriction_data":     32,
		"network_access_mode":         0,
		"subscriber_status":           0,
		"operator_determined_barring": 0,
		"subscribed_rau_tau_timer":    12,
		"__v":                         0,
	}

	_, err := cl.coll.UpdateOne(ctx,
		bson.M{"imsi": s.IMSI},
		bson.M{"$set": doc},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

// Delete removes a subscriber by IMSI.
func (cl *Client) Delete(ctx context.Context, imsi string) error {
	_, err := cl.coll.DeleteOne(ctx, bson.M{"imsi": imsi})
	return err
}
