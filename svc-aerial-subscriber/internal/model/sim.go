// Package model holds the SIM domain types + sentinel errors.
package model

import (
	"errors"
	"time"
)

var (
	ErrSIMNotFound = errors.New("sim not found")
	ErrSIMExists   = errors.New("sim with that IMSI already exists")
	ErrBadInput    = errors.New("invalid input")
)

// SIM card record.
type SIM struct {
	ID            string     `json:"id"`
	OrgID         string     `json:"org_id"`
	OwnerUserID   *string    `json:"owner_user_id,omitempty"`
	IMSI          string     `json:"imsi"`
	MSISDN        *string    `json:"msisdn,omitempty"`
	PLMNMcc       string     `json:"plmn_mcc"`
	PLMNMnc       string     `json:"plmn_mnc"`
	Ki            string     `json:"-"`          // never sent to clients in v1
	OPc           string     `json:"-"`          // never sent to clients in v1
	AMF           string     `json:"amf"`
	APN           string     `json:"apn"`
	SST           int16      `json:"sst"`
	SD            *string    `json:"sd,omitempty"`
	Status        string     `json:"status"`
	ProvisionedAt *time.Time `json:"provisioned_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// CreateSIMRequest from API. If imsi is empty we auto-assign the next MSIN.
// If ki/opc are empty we generate random ones.
type CreateSIMRequest struct {
	OwnerUserID *string `json:"owner_user_id,omitempty"`
	IMSI        string  `json:"imsi,omitempty"`
	MSISDN      *string `json:"msisdn,omitempty"`
	APN         string  `json:"apn,omitempty"`
	SST         int16   `json:"sst,omitempty"`
}
