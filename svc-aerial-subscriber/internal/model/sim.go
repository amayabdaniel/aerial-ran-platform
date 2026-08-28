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
	// ErrProvisioning marks a failure to provision the SIM into the 5G core
	// (Open5GS). The SIM row is stored but the UE cannot attach until it is
	// re-provisioned (Resume), so callers must not treat it as a clean success.
	ErrProvisioning = errors.New("sim provisioning into 5G core failed")
	// ErrDeprovisioning marks a failure to remove the SIM from the 5G core.
	// Reporting "suspended"/"terminated" while the UE can still attach would be
	// a dangerous lie (a line the operator believes is cut is still live), so
	// the status change is not applied and the failure is surfaced.
	ErrDeprovisioning = errors.New("sim de-provisioning from 5G core failed")
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
