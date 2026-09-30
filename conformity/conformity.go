// Package conformity provides typed access to the EAEU conformity assessment
// bodies registry. Transport and pagination are delegated to package eaeunion.
package conformity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	eaeunion "github.com/ReanSn0w/go-eaeunion-api"
)

const (
	Endpoint   = "https://tech.eaeunion.org/spd/find"
	Collection = "kbdread.service-prop-36-v_conformityAssessmentBodyInformationDetailsType_organization_table"
)

type Code struct {
	Value      string `json:"value"`
	CodeListID string `json:"codeListId,omitempty"`
}
type OID struct {
	Value string `json:"$oid"`
}
type Date struct {
	Value string `json:"$date"`
}
type Status struct {
	Code Code `json:"statusCode"`
}
type BodyDetails struct {
	Name      string            `json:"businessEntityUnitName"`
	Addresses []json.RawMessage `json:"addressV4Details,omitempty"`
	Officers  []json.RawMessage `json:"officerDetails,omitempty"`
}
type BusinessEntity struct {
	Name      string `json:"businessEntityName"`
	BriefName string `json:"businessEntityBriefName,omitempty"`
}
type Certificate struct {
	ID         string `json:"docId"`
	Created    Date   `json:"docCreationDate"`
	ValidUntil Date   `json:"docValidityDate"`
	Status     Status `json:"statusV2Details"`
}

// Record retains Raw to preserve fields not yet modeled by this package.
type Record struct {
	OID                OID               `json:"_id"`
	Country            Code              `json:"unifiedCountryCode"`
	AuthorityID        string            `json:"conformityAuthorityId"`
	Body               BodyDetails       `json:"conformityAssessmentBodyDetails"`
	BusinessEntity     BusinessEntity    `json:"businessEntityDetails"`
	Certificate        Certificate       `json:"accreditationCertificateDetails"`
	RecordStatus       Status            `json:"statusV2Details"`
	AccreditationAreas []json.RawMessage `json:"accreditationAreaDetails,omitempty"`
	Organizations      []string          `json:"organizations,omitempty"`
	Raw                json.RawMessage   `json:"-"`
}

type Page struct {
	Records          []Record
	TotalDocuments   *int64
	MatchedDocuments *int64
	ResponseSize     int64
}

// Filter combines named conditions and Extra with $and. NameContains escapes
// regexp metacharacters; NameRegex is passed through deliberately.
type Filter struct {
	AuthorityID         string
	Country             string
	NameContains        string
	NameRegex           string
	RecordStatus        string
	AccreditationStatus string
	Extra               any
}

type Client struct{ core *eaeunion.Client }

func NewClient(core *eaeunion.Client) (*Client, error) {
	if core == nil {
		return nil, errors.New("eaeunion client is required")
	}
	return &Client{core: core}, nil
}

func NewPublicClient() (*Client, error) {
	core, err := eaeunion.NewClient(eaeunion.Config{Endpoint: Endpoint})
	if err != nil {
		return nil, err
	}
	return NewClient(core)
}

func (c *Client) Find(ctx context.Context, filter Filter, query eaeunion.Query) (Page, error) {
	if c == nil || c.core == nil {
		return Page{}, errors.New("nil conformity client")
	}
	combined, err := filter.encode()
	if err != nil {
		return Page{}, err
	}
	if query.Filter != nil {
		combined, err = combine(combined, query.Filter)
		if err != nil {
			return Page{}, err
		}
	}
	query.Filter = combined
	page, err := c.core.Find(ctx, Collection, query)
	if err != nil {
		return Page{}, err
	}
	return decode(page)
}

func (c *Client) Walk(ctx context.Context, filter Filter, query eaeunion.Query, options eaeunion.WalkOptions, visit func(Page) (bool, error)) (eaeunion.WalkResult, error) {
	if c == nil || c.core == nil {
		return eaeunion.WalkResult{}, errors.New("nil conformity client")
	}
	if visit == nil {
		return eaeunion.WalkResult{}, errors.New("visit callback is required")
	}
	combined, err := filter.encode()
	if err != nil {
		return eaeunion.WalkResult{}, err
	}
	if query.Filter != nil {
		combined, err = combine(combined, query.Filter)
		if err != nil {
			return eaeunion.WalkResult{}, err
		}
	}
	query.Filter = combined
	return c.core.Walk(ctx, Collection, query, options, func(raw eaeunion.Page) (bool, error) {
		page, err := decode(raw)
		if err != nil {
			return false, err
		}
		return visit(page)
	})
}

var ErrNotFound = errors.New("conformity body not found")
var ErrAmbiguous = errors.New("multiple conformity bodies match identifier")

// GetByID optionally narrows the identifier with a country code. It never
// silently selects one record when the identifier is ambiguous.
func (c *Client) GetByID(ctx context.Context, id, country string) (Record, error) {
	if id == "" {
		return Record{}, errors.New("authority ID is required")
	}
	limit := 2
	page, err := c.Find(ctx, Filter{AuthorityID: id, Country: country}, eaeunion.Query{Limit: &limit})
	if err != nil {
		return Record{}, err
	}
	if len(page.Records) == 0 {
		return Record{}, ErrNotFound
	}
	if len(page.Records) > 1 || (page.MatchedDocuments != nil && *page.MatchedDocuments > 1) {
		return Record{}, ErrAmbiguous
	}
	return page.Records[0], nil
}

func decode(raw eaeunion.Page) (Page, error) {
	page := Page{Records: make([]Record, 0, len(raw.Result)), TotalDocuments: raw.TotalDocuments, MatchedDocuments: raw.MatchedDocuments, ResponseSize: raw.ResponseSize}
	for i, item := range raw.Result {
		var record Record
		if err := json.Unmarshal(item, &record); err != nil {
			return Page{}, fmt.Errorf("decode conformity record %d: %w", i, err)
		}
		record.Raw = append(json.RawMessage(nil), item...)
		page.Records = append(page.Records, record)
	}
	return page, nil
}

func (f Filter) encode() (any, error) {
	conditions := make([]any, 0, 7)
	if f.AuthorityID != "" {
		conditions = append(conditions, map[string]any{"conformityAuthorityId": f.AuthorityID})
	}
	if f.Country != "" {
		conditions = append(conditions, map[string]any{"unifiedCountryCode.value": f.Country})
	}
	if f.NameContains != "" {
		conditions = append(conditions, map[string]any{"conformityAssessmentBodyDetails.businessEntityUnitName": map[string]any{"$regex": regexp.QuoteMeta(f.NameContains)}})
	}
	if f.NameRegex != "" {
		conditions = append(conditions, map[string]any{"conformityAssessmentBodyDetails.businessEntityUnitName": map[string]any{"$regex": f.NameRegex}})
	}
	if f.RecordStatus != "" {
		conditions = append(conditions, map[string]any{"statusV2Details.statusCode.value": f.RecordStatus})
	}
	if f.AccreditationStatus != "" {
		conditions = append(conditions, map[string]any{"accreditationCertificateDetails.statusV2Details.statusCode.value": f.AccreditationStatus})
	}
	if f.Extra != nil {
		if err := validateObject(f.Extra); err != nil {
			return nil, fmt.Errorf("extra filter: %w", err)
		}
		conditions = append(conditions, f.Extra)
	}
	if len(conditions) == 0 {
		return map[string]any{}, nil
	}
	if len(conditions) == 1 {
		return conditions[0], nil
	}
	return map[string]any{"$and": conditions}, nil
}

func combine(left, right any) (any, error) {
	if err := validateObject(right); err != nil {
		return nil, fmt.Errorf("query filter: %w", err)
	}
	return map[string]any{"$and": []any{left, right}}, nil
}
func validateObject(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[0] != '{' || !json.Valid(b) {
		return errors.New("filter must be a JSON object")
	}
	return nil
}
